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
	"forward-stub/src/logx"
	"forward-stub/src/packet"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
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

// NewKafkaSender 负责该函数对应的核心逻辑，详见实现细节。
func NewKafkaSender(name string, sc config.SenderConfig) (*KafkaSender, error) {
	if strings.TrimSpace(sc.Topic) == "" {
		return nil, errors.New("kafka sender requires topic")
	}
	brs := splitCSV(sc.Remote)
	if len(brs) == 0 {
		return nil, errors.New("kafka sender requires brokers in remote/listen")
	}
	dialTimeout, err := kafkaDurationOrDefault(sc.DialTimeout, config.DefaultKafkaDialTimeout)
	if err != nil {
		return nil, fmt.Errorf("kafka sender dial_timeout 配置非法: %w", err)
	}
	requestTimeout, err := kafkaDurationOrDefault(sc.RequestTimeout, config.DefaultKafkaSenderRequestTimeout)
	if err != nil {
		return nil, fmt.Errorf("kafka sender request_timeout 配置非法: %w", err)
	}
	retryTimeout, err := kafkaDurationOrDefault(sc.RetryTimeout, config.DefaultKafkaRetryTimeout)
	if err != nil {
		return nil, fmt.Errorf("kafka sender retry_timeout 配置非法: %w", err)
	}
	retryBackoff, err := kafkaDurationOrDefault(sc.RetryBackoff, config.DefaultKafkaRetryBackoff)
	if err != nil {
		return nil, fmt.Errorf("kafka sender retry_backoff 配置非法: %w", err)
	}
	connIdleTimeout, err := kafkaDurationOrDefault(sc.ConnIdleTimeout, config.DefaultKafkaConnIdleTimeout)
	if err != nil {
		return nil, fmt.Errorf("kafka sender conn_idle_timeout 配置非法: %w", err)
	}
	metadataMaxAge, err := kafkaDurationOrDefault(sc.MetadataMaxAge, config.DefaultKafkaMetadataMaxAge)
	if err != nil {
		return nil, fmt.Errorf("kafka sender metadata_max_age 配置非法: %w", err)
	}
	partitioner, err := kafkaPartitioner(sc.Partitioner)
	if err != nil {
		return nil, err
	}
	opts := []kgo.Opt{
		kgo.SeedBrokers(brs...),
		kgo.RequiredAcks(kafkaRequiredAcks(sc.Acks.Int())),
		kgo.DialTimeout(dialTimeout),
		kgo.ProduceRequestTimeout(requestTimeout),
		kgo.RetryTimeout(retryTimeout),
		kgo.RetryBackoffFn(func(int) time.Duration { return retryBackoff }),
		kgo.ConnIdleTimeout(connIdleTimeout),
		kgo.MetadataMaxAge(metadataMaxAge),
		kgo.RecordPartitioner(partitioner),
		kgo.ProducerBatchMaxBytes(int32(kafkaIntDefault(sc.BatchMaxBytes, 1<<20))),
		kgo.ProducerLinger(time.Duration(kafkaIntDefault(sc.LingerMS, 1)) * time.Millisecond),
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
	if codec, enabled, err := kafkaCompressionCodec(sc.Compression, sc.CompressionLevel); err != nil {
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
	if mech, err := buildKafkaSASLMechanism(sc.SASLMechanism, sc.Username, sc.Password); err != nil {
		return nil, err
	} else if mech != nil {
		opts = append(opts, kgo.SASL(mech))
	}
	concurrency := kafkaIntDefault(sc.Concurrency, 1)
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

// Name 负责该函数对应的核心逻辑，详见实现细节。
func (s *KafkaSender) Name() string { return s.name }

// Key 负责该函数对应的核心逻辑，详见实现细节。
func (s *KafkaSender) Key() string { return "kafka|" + strings.Join(s.brokers, ",") + "|" + s.topic }

// Send 负责该函数对应的核心逻辑，详见实现细节。
func (s *KafkaSender) Send(ctx context.Context, p *packet.Packet) error {
	idx := nextShardIndex(&s.nextIdx, s.shardMask)
	cli := s.clients[idx].Load()
	if cli == nil {
		return errors.New("kafka sender closed")
	}
	rec := &kgo.Record{Topic: s.topic, Value: p.Payload, Key: kafkaRecordKey(s.recordKey, s.keySource, p)}
	res := cli.ProduceSync(ctx, rec)
	if err := res.FirstErr(); err != nil {
		logx.L().Warnw("Kafka发送失败", "发送端", s.name, "主题", s.topic, "错误", err)
		return err
	}
	return nil
}

// Close 负责该函数对应的核心逻辑，详见实现细节。
func (s *KafkaSender) Close(ctx context.Context) error {
	for i := 0; i < s.concurrency; i++ {
		cli := s.clients[i].Swap(nil)
		if cli != nil {
			cli.Close()
		}
	}
	return nil
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func kafkaIntDefault(v, d int) int {
	if v <= 0 {
		return d
	}
	return v
}

func kafkaRequiredAcks(acks int) kgo.Acks {
	switch acks {
	case 0:
		return kgo.NoAck()
	case 1:
		return kgo.LeaderAck()
	default:
		return kgo.AllISRAcks()
	}
}

func kafkaCompressionCodec(v string, level int) (kgo.CompressionCodec, bool, error) {
	withLevel := func(codec kgo.CompressionCodec) kgo.CompressionCodec {
		if level == 0 {
			return codec
		}
		return codec.WithLevel(level)
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "none":
		return kgo.NoCompression(), false, nil
	case "gzip":
		return withLevel(kgo.GzipCompression()), true, nil
	case "snappy":
		return kgo.SnappyCompression(), true, nil
	case "lz4":
		return withLevel(kgo.Lz4Compression()), true, nil
	case "zstd":
		return withLevel(kgo.ZstdCompression()), true, nil
	default:
		return kgo.NoCompression(), false, fmt.Errorf("kafka compression %s unsupported", v)
	}
}

func kafkaPartitioner(v string) (kgo.Partitioner, error) {
	switch strings.TrimSpace(v) {
	case "", "sticky":
		return kgo.StickyPartitioner(), nil
	case "round_robin":
		return kgo.RoundRobinPartitioner(), nil
	case "hash_key":
		return kgo.StickyKeyPartitioner(nil), nil
	default:
		return nil, fmt.Errorf("kafka partitioner %s unsupported", v)
	}
}

func kafkaRecordKey(fixedKey []byte, source string, p *packet.Packet) []byte {
	if len(fixedKey) > 0 {
		return append([]byte(nil), fixedKey...)
	}
	if p == nil {
		return nil
	}
	switch source {
	case "":
		return nil
	case "payload":
		return append([]byte(nil), p.Payload...)
	case "match_key":
		return []byte(p.Meta.MatchKey)
	case "remote":
		return []byte(p.Meta.Remote)
	case "local":
		return []byte(p.Meta.Local)
	case "file_name":
		return []byte(p.Meta.FileName)
	case "file_path":
		return []byte(p.Meta.FilePath)
	case "transfer_id":
		return []byte(p.Meta.TransferID)
	case "route_sender":
		return []byte(p.Meta.RouteSender)
	default:
		return nil
	}
}

func kafkaDurationOrDefault(value, fallback string) (time.Duration, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		raw = fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, fmt.Errorf("duration must be > 0")
	}
	return d, nil
}

type kafkaPlainMechanism struct {
	username string
	password string
}

func (m kafkaPlainMechanism) Name() string { return "PLAIN" }

func (m kafkaPlainMechanism) Authenticate(_ context.Context, _ string) (sasl.Session, []byte, error) {
	msg := []byte("\x00" + m.username + "\x00" + m.password)
	return kafkaPlainSession{}, msg, nil
}

type kafkaPlainSession struct{}

func (kafkaPlainSession) Challenge(_ []byte) (bool, []byte, error) {
	return true, nil, nil
}

func buildKafkaSASLMechanism(mechanism, username, password string) (sasl.Mechanism, error) {
	mech := strings.ToUpper(strings.TrimSpace(mechanism))
	u := strings.TrimSpace(username)
	p := strings.TrimSpace(password)
	if mech == "" && (u != "" || p != "") {
		mech = "PLAIN"
	}
	if mech == "" {
		return nil, nil
	}
	if u == "" || p == "" {
		return nil, fmt.Errorf("kafka sasl %s requires username and password", mech)
	}
	if mech != "PLAIN" {
		return nil, fmt.Errorf("kafka sasl mechanism %s unsupported, only PLAIN is supported", mech)
	}
	return kafkaPlainMechanism{username: u, password: p}, nil
}
