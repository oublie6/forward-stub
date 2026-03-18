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

// KafkaSender describes sender-level state used by the forwarding architecture.
type KafkaSender struct {
	name        string
	brokers     []string
	topic       string
	concurrency int
	shardMask   int

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
	opts := []kgo.Opt{
		kgo.SeedBrokers(brs...),
		kgo.RequiredAcks(kafkaRequiredAcks(sc.Acks.Int())),
		kgo.RecordPartitioner(kgo.StickyKeyPartitioner(nil)),
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
	if codec, enabled, err := kafkaCompressionCodec(sc.Compression); err != nil {
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
	rec := &kgo.Record{Topic: s.topic, Value: p.Payload}
	res := cli.ProduceSync(ctx, rec)
	if err := res.FirstErr(); err != nil {
		logx.L().Warnw("kafka sender produce failed", "sender", s.name, "topic", s.topic, "error", err)
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

// splitCSV is a package-local helper used by kafka.go.
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

// kafkaIntDefault is a package-local helper used by kafka.go.
func kafkaIntDefault(v, d int) int {
	if v <= 0 {
		return d
	}
	return v
}

// kafkaRequiredAcks is a package-local helper used by kafka.go.
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

// kafkaCompressionCodec is a package-local helper used by kafka.go.
func kafkaCompressionCodec(v string) (kgo.CompressionCodec, bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "none":
		return kgo.NoCompression(), false, nil
	case "gzip":
		return kgo.GzipCompression(), true, nil
	case "snappy":
		return kgo.SnappyCompression(), true, nil
	case "lz4":
		return kgo.Lz4Compression(), true, nil
	case "zstd":
		return kgo.ZstdCompression(), true, nil
	default:
		return kgo.NoCompression(), false, fmt.Errorf("kafka compression %s unsupported", v)
	}
}

// kafkaPlainMechanism stores package-local state used by kafka.go.
type kafkaPlainMechanism struct {
	username string
	password string
}

// Name provides sender-level behavior used by the runtime pipeline.
func (m kafkaPlainMechanism) Name() string { return "PLAIN" }

// Authenticate provides sender-level behavior used by the runtime pipeline.
func (m kafkaPlainMechanism) Authenticate(_ context.Context, _ string) (sasl.Session, []byte, error) {
	msg := []byte("\x00" + m.username + "\x00" + m.password)
	return kafkaPlainSession{}, msg, nil
}

// kafkaPlainSession stores package-local state used by kafka.go.
type kafkaPlainSession struct{}

// Challenge provides sender-level behavior used by the runtime pipeline.
func (kafkaPlainSession) Challenge(_ []byte) (bool, []byte, error) {
	return true, nil, nil
}

// buildKafkaSASLMechanism is a package-local helper used by kafka.go.
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
