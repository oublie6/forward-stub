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
	// name: 逻辑名称，对应配置 senders 的 key。
	name string
	// brokers: Kafka broker 列表，来自配置 remote（CSV）。
	brokers []string
	// topic: 目标主题。
	topic string

	mu sync.Mutex
	// client: franz-go producer 客户端，实例级复用以减少连接与元数据开销。
	client *kgo.Client
}

func NewKafkaSender(name, brokers, topic string) (*KafkaSender, error) {
	// topic 与 brokers 必填。
	if strings.TrimSpace(topic) == "" {
		return nil, errors.New("kafka sender requires topic")
	}
	brs := splitCSV(brokers)
	if len(brs) == 0 {
		return nil, errors.New("kafka sender requires brokers in remote/listen")
	}
	// 生产参数说明：
	// - RequiredAcks(AllISRAcks): 可靠性优先，避免 leader-only ack 导致数据风险。
	// - StickyKeyPartitioner: 对无 key 流量提升批量聚合效率，提升吞吐。
	// - ProducerBatchMaxBytes(1MiB): 放大单批次上限，减少请求次数。
	// - ProducerLinger(1ms): 微小聚合窗口，兼顾吞吐与低延迟。
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
	// 这里使用 ProduceSync：
	// - 优点：调用侧语义简单，错误可即时反馈到任务链路；
	// - 代价：吞吐上限通常低于纯异步生产（若后续需要极限吞吐可演进）。
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
