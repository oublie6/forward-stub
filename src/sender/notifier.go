package sender

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"forward-stub/src/config"
	"forward-stub/src/kafkautil"
	"forward-stub/src/packet"
	"forward-stub/src/skydds"

	"github.com/twmb/franz-go/pkg/kgo"
)

const fileReadyEventType = "file_ready"

// FileReadyEvent 是 OSS sender 完成 multipart commit 后的 file_ready 通知事件。
//
// 触发时机：
//   - OSS sender 在 CompleteMultipartUpload 成功后构造。
//
// 事件中同时保留来源文件信息、OSS 目标位置，以及下游拉取对象所需的 fetch_* 字段。
type FileReadyEvent struct {
	EventType    string    `json:"event_type"`
	TransferID   string    `json:"transfer_id"`
	SourceProto  string    `json:"source_proto"`
	SourcePath   string    `json:"source_path"`
	TargetProto  string    `json:"target_proto"`
	FileName     string    `json:"file_name"`
	FilePath     string    `json:"file_path"`
	TotalSize    int64     `json:"total_size"`
	SenderName   string    `json:"sender_name"`
	ReceiverName string    `json:"receiver_name"`
	ReadyAt      time.Time `json:"ready_at"`

	FetchProtocol string `json:"fetch_protocol"`
	FetchHost     string `json:"fetch_host,omitempty"`
	FetchPort     string `json:"fetch_port,omitempty"`
	FetchPath     string `json:"fetch_path,omitempty"`
	FetchEndpoint string `json:"fetch_endpoint,omitempty"`
	FetchBucket   string `json:"fetch_bucket,omitempty"`
	FetchKey      string `json:"fetch_key,omitempty"`
}

// FileReadyNotifier 抽象 OSS commit success 后的通知通道。
// 实现必须把 NotifyFileReady 视为“OSS CompleteMultipartUpload 已成功后的后置动作”；
// 返回错误不会回滚已提交对象，只会让 OSS sender 将错误返回给上层并记录日志，后续可接 outbox/补发机制。
type FileReadyNotifier interface {
	NotifyFileReady(ctx context.Context, event FileReadyEvent) error
	Close(ctx context.Context) error
}

func buildOSSCommitNotifiers(cfgs config.NotifyOnSuccessConfigs) ([]FileReadyNotifier, error) {
	out := make([]FileReadyNotifier, 0, len(cfgs))
	for _, nc := range cfgs {
		n, err := buildOSSCommitNotifier(nc)
		if err != nil {
			for _, old := range out {
				_ = old.Close(context.Background())
			}
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

func buildOSSCommitNotifier(nc config.NotifyOnSuccessConfig) (FileReadyNotifier, error) {
	switch strings.TrimSpace(nc.Type) {
	case "kafka":
		return NewKafkaCommitNotifier(nc)
	case "dds_skydds":
		return NewSkyDDSCommitNotifier(nc)
	default:
		return nil, fmt.Errorf("notify_on_success unsupported type %s", nc.Type)
	}
}

type KafkaCommitNotifier struct {
	client    *kgo.Client
	topic     string
	keySource string
}

// NewKafkaCommitNotifier 构造 OSS commit success 后基于 Kafka 的 file_ready 通知器。
// 调用时只创建 kgo client，不会主动发起 Produce；真正网络发送发生在 NotifyFileReady。
func NewKafkaCommitNotifier(nc config.NotifyOnSuccessConfig) (*KafkaCommitNotifier, error) {
	brs := kafkautil.SplitCSV(nc.Remote)
	if len(brs) == 0 {
		return nil, fmt.Errorf("kafka commit notifier requires remote")
	}
	dialTimeout, err := kafkautil.DurationOrDefault(nc.DialTimeout, config.DefaultKafkaDialTimeout)
	if err != nil {
		return nil, fmt.Errorf("kafka commit notifier dial_timeout 配置非法: %w", err)
	}
	requestTimeout, err := kafkautil.DurationOrDefault(nc.RequestTimeout, config.DefaultKafkaSenderRequestTimeout)
	if err != nil {
		return nil, fmt.Errorf("kafka commit notifier request_timeout 配置非法: %w", err)
	}
	retryTimeout, err := kafkautil.DurationOrDefault(nc.RetryTimeout, config.DefaultKafkaRetryTimeout)
	if err != nil {
		return nil, fmt.Errorf("kafka commit notifier retry_timeout 配置非法: %w", err)
	}
	retryBackoff, err := kafkautil.DurationOrDefault(nc.RetryBackoff, config.DefaultKafkaRetryBackoff)
	if err != nil {
		return nil, fmt.Errorf("kafka commit notifier retry_backoff 配置非法: %w", err)
	}
	connIdleTimeout, err := kafkautil.DurationOrDefault(nc.ConnIdleTimeout, config.DefaultKafkaConnIdleTimeout)
	if err != nil {
		return nil, fmt.Errorf("kafka commit notifier conn_idle_timeout 配置非法: %w", err)
	}
	metadataMaxAge, err := kafkautil.DurationOrDefault(nc.MetadataMaxAge, config.DefaultKafkaMetadataMaxAge)
	if err != nil {
		return nil, fmt.Errorf("kafka commit notifier metadata_max_age 配置非法: %w", err)
	}
	opts := []kgo.Opt{
		kgo.SeedBrokers(brs...),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.DialTimeout(dialTimeout),
		kgo.ProduceRequestTimeout(requestTimeout),
		kgo.RetryTimeout(retryTimeout),
		kgo.RetryBackoffFn(func(int) time.Duration { return retryBackoff }),
		kgo.ConnIdleTimeout(connIdleTimeout),
		kgo.MetadataMaxAge(metadataMaxAge),
	}
	if nc.ClientID != "" {
		opts = append(opts, kgo.ClientID(nc.ClientID))
	}
	if nc.TLS {
		opts = append(opts, kgo.DialTLSConfig(&tls.Config{InsecureSkipVerify: nc.TLSSkipVerify}))
	}
	if mech, err := kafkautil.BuildSASLMechanism(nc.SASLMechanism, nc.Username, nc.Password); err != nil {
		return nil, err
	} else if mech != nil {
		opts = append(opts, kgo.SASL(mech))
	}
	cli, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, err
	}
	return &KafkaCommitNotifier{client: cli, topic: nc.Topic, keySource: strings.TrimSpace(nc.RecordKeySource)}, nil
}

// NotifyFileReady 将 OSS file_ready 事件序列化为 JSON 并同步 Produce 到 Kafka。
// record key 由 record_key_source 决定；未配置时 key 为空，交由 Kafka 分区策略处理。
func (n *KafkaCommitNotifier) NotifyFileReady(ctx context.Context, event FileReadyEvent) error {
	b, err := json.Marshal(event)
	if err != nil {
		return err
	}
	rec := &kgo.Record{Topic: n.topic, Key: fileReadyRecordKey(n.keySource, event), Value: b}
	return n.client.ProduceSync(ctx, rec).FirstErr()
}

func (n *KafkaCommitNotifier) Close(context.Context) error {
	if n.client != nil {
		n.client.Close()
	}
	return nil
}

type SkyDDSCommitNotifier struct {
	writer skydds.Writer
}

var skyddsCommitWriterFactory = skydds.NewWriter

// NewSkyDDSCommitNotifier 构造 OSS commit success 后基于 SkyDDS OctetMsg 的 file_ready 通知器。
// 当前只支持 message_model=octet，避免 commit 通知再引入批量聚合导致“文件已就绪但通知延迟”。
func NewSkyDDSCommitNotifier(nc config.NotifyOnSuccessConfig) (*SkyDDSCommitNotifier, error) {
	if strings.ToLower(strings.TrimSpace(nc.MessageModel)) != "octet" {
		return nil, fmt.Errorf("skydds commit notifier only supports message_model=octet")
	}
	w, err := skyddsCommitWriterFactory(skydds.CommonOptions{
		DCPSConfigFile: nc.DCPSConfigFile,
		DomainID:       nc.DomainID,
		TopicName:      nc.TopicName,
		MessageModel:   "octet",
	})
	if err != nil {
		return nil, fmt.Errorf("new skydds commit notifier writer: %w", err)
	}
	return &SkyDDSCommitNotifier{writer: w}, nil
}

// NotifyFileReady 将 OSS file_ready 事件序列化为 JSON 后写入 SkyDDS。
func (n *SkyDDSCommitNotifier) NotifyFileReady(_ context.Context, event FileReadyEvent) error {
	b, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return n.writer.Write(b)
}

func (n *SkyDDSCommitNotifier) Close(context.Context) error {
	if n.writer == nil {
		return nil
	}
	return n.writer.Close()
}

func notifyOSSCommitSuccess(ctx context.Context, notifiers []FileReadyNotifier, event FileReadyEvent) error {
	var errs []error
	for i, n := range notifiers {
		if err := n.NotifyFileReady(ctx, event); err != nil {
			// TODO: 增加持久化补发表，避免 OSS commit 成功后通知失败只能依赖上层重试。
			errs = append(errs, fmt.Errorf("notifier[%d]: %w", i, err))
		}
	}
	return errors.Join(errs...)
}

func baseFileReadyEvent(p *packet.Packet, senderName, targetProto, finalPath string) FileReadyEvent {
	return FileReadyEvent{
		EventType:    fileReadyEventType,
		TransferID:   p.Meta.TransferID,
		SourceProto:  protoName(p.Meta.Proto),
		SourcePath:   p.Meta.FilePath,
		TargetProto:  targetProto,
		FileName:     finalFileName(p),
		FilePath:     finalPath,
		TotalSize:    p.Meta.TotalSize,
		SenderName:   senderName,
		ReceiverName: p.Meta.ReceiverName,
		ReadyAt:      time.Now().UTC(),
	}
}

func ossFileReadyEvent(p *packet.Packet, senderName, endpoint, bucket, key string) FileReadyEvent {
	event := baseFileReadyEvent(p, senderName, "oss", key)
	event.FetchProtocol = "oss"
	event.FetchEndpoint = endpoint
	event.FetchBucket = bucket
	event.FetchKey = key
	return event
}

func finalFileName(p *packet.Packet) string {
	if strings.TrimSpace(p.Meta.TargetFileName) != "" {
		return p.Meta.TargetFileName
	}
	if strings.TrimSpace(p.Meta.TargetFilePath) != "" {
		return pathBase(p.Meta.TargetFilePath)
	}
	if strings.TrimSpace(p.Meta.FileName) != "" {
		return p.Meta.FileName
	}
	return pathBase(p.Meta.FilePath)
}

func protoName(proto packet.Proto) string {
	switch proto {
	case packet.ProtoUDP:
		return "udp"
	case packet.ProtoTCP:
		return "tcp"
	case packet.ProtoKafka:
		return "kafka"
	case packet.ProtoSFTP:
		return "sftp"
	case packet.ProtoSkyDDS:
		return "dds_skydds"
	case packet.ProtoLocal:
		return "local"
	case packet.ProtoOSS:
		return "oss"
	default:
		return "unknown"
	}
}

func fileReadyRecordKey(source string, event FileReadyEvent) []byte {
	switch source {
	case "transfer_id":
		return []byte(event.TransferID)
	case "file_path":
		return []byte(event.FilePath)
	case "file_name":
		return []byte(event.FileName)
	case "sender_name":
		return []byte(event.SenderName)
	case "receiver_name":
		return []byte(event.ReceiverName)
	case "fetch_path":
		if event.FetchPath != "" {
			return []byte(event.FetchPath)
		}
		return []byte(event.FetchKey)
	default:
		return nil
	}
}
