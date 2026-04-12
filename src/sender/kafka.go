// kafka.go 实现基于 franz-go 的 Kafka 发送端。
package sender

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"forward-stub/src/config"
	"forward-stub/src/kafkautil"
	"forward-stub/src/logx"
	"forward-stub/src/packet"

	"github.com/twmb/franz-go/pkg/kgo"
)

type KafkaSender struct {
	name        string
	brokers     []string
	topic       string
	concurrency int
	shardMask   int
	recordKey   []byte
	keySource   string

	clients []atomic.Pointer[kgo.Client]
	nextIdx atomic.Uint64
}

// NewKafkaSender 创建 franz-go producer 分片。
// 所有 Kafka 选项在构造阶段编译完成，Send 热路径只选择 client、构造 record 并同步发送。
func NewKafkaSender(name string, sc config.SenderConfig) (*KafkaSender, error) {
	if strings.TrimSpace(sc.Topic) == "" {
		return nil, errors.New("kafka sender requires topic")
	}
	brs := kafkautil.SplitCSV(sc.Remote)
	if len(brs) == 0 {
		return nil, errors.New("kafka sender requires brokers in remote/listen")
	}
	dialTimeout, err := kafkautil.DurationOrDefault(sc.DialTimeout, config.DefaultKafkaDialTimeout)
	if err != nil {
		return nil, fmt.Errorf("kafka sender dial_timeout 配置非法: %w", err)
	}
	requestTimeout, err := kafkautil.DurationOrDefault(sc.RequestTimeout, config.DefaultKafkaSenderRequestTimeout)
	if err != nil {
		return nil, fmt.Errorf("kafka sender request_timeout 配置非法: %w", err)
	}
	retryTimeout, err := kafkautil.DurationOrDefault(sc.RetryTimeout, config.DefaultKafkaRetryTimeout)
	if err != nil {
		return nil, fmt.Errorf("kafka sender retry_timeout 配置非法: %w", err)
	}
	retryBackoff, err := kafkautil.DurationOrDefault(sc.RetryBackoff, config.DefaultKafkaRetryBackoff)
	if err != nil {
		return nil, fmt.Errorf("kafka sender retry_backoff 配置非法: %w", err)
	}
	connIdleTimeout, err := kafkautil.DurationOrDefault(sc.ConnIdleTimeout, config.DefaultKafkaConnIdleTimeout)
	if err != nil {
		return nil, fmt.Errorf("kafka sender conn_idle_timeout 配置非法: %w", err)
	}
	metadataMaxAge, err := kafkautil.DurationOrDefault(sc.MetadataMaxAge, config.DefaultKafkaMetadataMaxAge)
	if err != nil {
		return nil, fmt.Errorf("kafka sender metadata_max_age 配置非法: %w", err)
	}
	partitioner, err := kafkautil.Partitioner(sc.Partitioner)
	if err != nil {
		return nil, err
	}
	opts := []kgo.Opt{
		kgo.SeedBrokers(brs...),
		kgo.RequiredAcks(kafkautil.RequiredAcks(sc.Acks.Int())),
		kgo.DialTimeout(dialTimeout),
		kgo.ProduceRequestTimeout(requestTimeout),
		kgo.RetryTimeout(retryTimeout),
		kgo.RetryBackoffFn(func(int) time.Duration { return retryBackoff }),
		kgo.ConnIdleTimeout(connIdleTimeout),
		kgo.MetadataMaxAge(metadataMaxAge),
		kgo.RecordPartitioner(partitioner),
		kgo.ProducerBatchMaxBytes(int32(kafkautil.IntDefault(sc.BatchMaxBytes, 1<<20))),
		kgo.ProducerLinger(time.Duration(kafkautil.IntDefault(sc.LingerMS, 1)) * time.Millisecond),
	}
	if sc.MaxBufferedBytes > 0 {
		opts = append(opts, kgo.MaxBufferedBytes(sc.MaxBufferedBytes))
	}
	if sc.MaxBufferedRecords > 0 {
		opts = append(opts, kgo.MaxBufferedRecords(sc.MaxBufferedRecords))
	}
	if v := strings.TrimSpace(sc.ClientID); v != "" {
		opts = append(opts, kgo.ClientID(v))
	}
	if codec, enabled, err := kafkautil.CompressionCodec(sc.Compression, sc.CompressionLevel); err != nil {
		return nil, err
	} else if enabled {
		opts = append(opts, kgo.ProducerBatchCompression(codec))
	}
	if sc.TLS {
		opts = append(opts, kgo.DialTLSConfig(&tls.Config{InsecureSkipVerify: sc.TLSSkipVerify}))
	}

	idempotent := true
	if sc.Idempotent != nil {
		idempotent = *sc.Idempotent
	}
	if !idempotent {
		opts = append(opts, kgo.DisableIdempotentWrite())
	}
	if sc.Retries > 0 {
		opts = append(opts, kgo.RecordRetries(sc.Retries))
	}
	if sc.MaxInFlightRequestsPerConnection > 0 {
		opts = append(opts, kgo.MaxProduceRequestsInflightPerBroker(sc.MaxInFlightRequestsPerConnection))
	}
	if mech, err := kafkautil.BuildSASLMechanism(sc.SASLMechanism, sc.Username, sc.Password); err != nil {
		return nil, err
	} else if mech != nil {
		opts = append(opts, kgo.SASL(mech))
	}
	concurrency := kafkautil.IntDefault(sc.Concurrency, 1)
	s := &KafkaSender{
		name:        name,
		brokers:     brs,
		topic:       sc.Topic,
		concurrency: concurrency,
		shardMask:   concurrency - 1,
		recordKey:   []byte(sc.RecordKey),
		keySource:   strings.TrimSpace(sc.RecordKeySource),
		clients:     make([]atomic.Pointer[kgo.Client], concurrency),
	}
	for i := 0; i < concurrency; i++ {
		cli, err := kgo.NewClient(opts...)
		if err != nil {
			_ = s.Close(context.Background())
			return nil, err
		}
		s.clients[i].Store(cli)
	}
	return s, nil
}

// Name 返回 sender 配置名。
func (s *KafkaSender) Name() string { return s.name }

// Key 返回 Kafka broker+topic 身份键。
func (s *KafkaSender) Key() string { return "kafka|" + strings.Join(s.brokers, ",") + "|" + s.topic }

// Send 同步生产一条 Kafka record。
// record key 来源已在配置校验阶段约束，运行时只按预编译字段取值。
func (s *KafkaSender) Send(ctx context.Context, p *packet.Packet) error {
	idx := nextShardIndex(&s.nextIdx, s.shardMask)
	cli := s.clients[idx].Load()
	if cli == nil {
		return errors.New("kafka sender closed")
	}
	rec := &kgo.Record{Topic: s.topic, Value: p.Payload, Key: kafkautil.RecordKey(s.recordKey, s.keySource, p)}
	res := cli.ProduceSync(ctx, rec)
	if err := res.FirstErr(); err != nil {
		logx.L().Warnw("Kafka发送失败", "发送端", s.name, "主题", s.topic, "错误", err)
		return err
	}
	return nil
}

// Close 关闭所有 producer 分片；允许重复调用。
func (s *KafkaSender) Close(ctx context.Context) error {
	for i := 0; i < s.concurrency; i++ {
		cli := s.clients[i].Swap(nil)
		if cli != nil {
			cli.Close()
		}
	}
	return nil
}
