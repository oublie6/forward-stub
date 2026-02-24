package sender

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"forword-stub/src/logx"
	"forword-stub/src/packet"

	"github.com/twmb/franz-go/pkg/kgo"
	"go.uber.org/zap/zapcore"
)

type KafkaSender struct {
	name    string
	brokers []string
	topic   string

	mu     sync.Mutex
	client *kgo.Client
}

func NewKafkaSender(name, brokers, topic string) (*KafkaSender, error) {
	if strings.TrimSpace(topic) == "" {
		return nil, errors.New("kafka sender requires topic")
	}
	brs := splitCSV(brokers)
	if len(brs) == 0 {
		return nil, errors.New("kafka sender requires brokers in remote/listen")
	}
	opts := []kgo.Opt{
		kgo.SeedBrokers(brs...),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.RecordPartitioner(kgo.StickyKeyPartitioner(nil)),
		kgo.ProducerBatchMaxBytes(1 << 20),
		kgo.ProducerLinger(1 * time.Millisecond),
	}
	cli, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, err
	}
	return &KafkaSender{name: name, brokers: brs, topic: topic, client: cli}, nil
}

func (s *KafkaSender) Name() string { return s.name }
func (s *KafkaSender) Key() string  { return "kafka|" + strings.Join(s.brokers, ",") + "|" + s.topic }

func (s *KafkaSender) Send(ctx context.Context, p *packet.Packet) error {
	s.mu.Lock()
	cli := s.client
	s.mu.Unlock()
	if cli == nil {
		return errors.New("kafka sender closed")
	}
	rec := &kgo.Record{Topic: s.topic, Value: p.Payload}
	res := cli.ProduceSync(ctx, rec)
	if err := res.FirstErr(); err != nil {
		if logx.Enabled(zapcore.WarnLevel) {
			logx.L().Warnw("kafka sender produce failed", "sender", s.name, "topic", s.topic, "error", err)
		}
		return err
	}
	return nil
}

func (s *KafkaSender) Close(ctx context.Context) error {
	s.mu.Lock()
	cli := s.client
	s.client = nil
	s.mu.Unlock()
	if cli != nil {
		cli.Close()
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
