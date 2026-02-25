// kafka.go 实现基于 Sarama consumer group 的 Kafka 接收端。
package receiver

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

type KafkaReceiver struct {
	// name: 逻辑名称，对应配置 receivers 的 key。
	name string
	// brokers: Kafka broker 列表，来自配置 listen（CSV）。
	brokers []string
	// topic: 消费主题。
	topic string
	// groupID: 消费组，用于分区负载均衡与位点管理。
	groupID string

	onPacket func(*packet.Packet)

	mu sync.Mutex
	// client: franz-go 客户端；Start 创建，Stop/退出时关闭。
	client *kgo.Client
	// cancel: 用于中断 PollFetches。
	cancel context.CancelFunc
	// done: Start 退出信号，供 Stop 等待优雅结束。
	done chan struct{}

	stats *logx.TrafficCounter
}

// NewKafkaReceiver 负责该函数对应的核心逻辑，详见实现细节。
func NewKafkaReceiver(name, brokers, topic, groupID string) (*KafkaReceiver, error) {
	// topic 与 brokers 是必填。groupID 允许空并自动生成，便于开箱即用。
	if strings.TrimSpace(topic) == "" {
		return nil, errors.New("kafka receiver requires topic")
	}
	if strings.TrimSpace(groupID) == "" {
		groupID = "forword-stub-" + name
	}
	brs := splitCSV(brokers)
	if len(brs) == 0 {
		return nil, errors.New("kafka receiver requires brokers in listen/remote")
	}
	return &KafkaReceiver{name: name, brokers: brs, topic: topic, groupID: groupID}, nil
}

// Name 负责该函数对应的核心逻辑，详见实现细节。
func (r *KafkaReceiver) Name() string { return r.name }

// Key 负责该函数对应的核心逻辑，详见实现细节。
func (r *KafkaReceiver) Key() string {
	return "kafka|" + strings.Join(r.brokers, ",") + "|" + r.groupID + "|" + r.topic
}

// Start 负责该函数对应的核心逻辑，详见实现细节。
func (r *KafkaReceiver) Start(ctx context.Context, onPacket func(*packet.Packet)) error {
	r.onPacket = onPacket

	// 消费参数说明：
	// - ConsumerGroup + ConsumeTopics: 使用 group 消费模型，支持重平衡。
	// - FetchMinBytes(1): 低流量时尽快返回，降低端到端延迟。
	// - FetchMaxBytes(16MiB): 提高批量拉取上限，提升吞吐。
	// - FetchMaxWait(100ms): 服务端最长等待窗口，平衡延迟与批量效率。
	opts := []kgo.Opt{
		kgo.SeedBrokers(r.brokers...),
		kgo.ConsumerGroup(r.groupID),
		kgo.ConsumeTopics(r.topic),
		kgo.FetchMinBytes(1),
		kgo.FetchMaxBytes(16 << 20),
		kgo.FetchMaxWait(100 * time.Millisecond),
	}
	cli, err := kgo.NewClient(opts...)
	if err != nil {
		return err
	}

	r.mu.Lock()
	r.client = cli
	r.done = make(chan struct{})
	rctx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	if logx.Enabled(zapcore.InfoLevel) {
		r.stats = logx.AcquireTrafficCounter(
			"receiver traffic stats",
			"role", "receiver",
			"receiver", r.Name(),
			"receiver_key", r.Key(),
			"proto", "kafka",
		)
	}
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		if r.stats != nil {
			r.stats.Close()
			r.stats = nil
		}
		if r.client != nil {
			r.client.Close()
			r.client = nil
		}
		if r.cancel != nil {
			r.cancel()
			r.cancel = nil
		}
		if r.done != nil {
			close(r.done)
			r.done = nil
		}
		r.mu.Unlock()
	}()

	for {
		// PollFetches 为阻塞拉取；被 rctx cancel 时会尽快返回。
		fetches := cli.PollFetches(rctx)
		if err := fetches.Err0(); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			if logx.Enabled(zapcore.WarnLevel) {
				logx.L().Warnw("kafka receiver poll error", "receiver", r.name, "error", err)
			}
			continue
		}

		var anyRecord bool
		fetches.EachRecord(func(rec *kgo.Record) {
			anyRecord = true
			if r.stats != nil {
				r.stats.AddBytes(len(rec.Value))
			}
			// 数据生命周期约定：
			// 从 franz-go record 拷贝到 packet 池化内存，避免上游复用/释放导致悬挂引用。
			payload, rel := packet.CopyFrom(rec.Value)
			r.onPacket(&packet.Packet{
				Payload: payload,
				Meta: packet.Meta{
					Proto:  packet.ProtoKafka,
					Remote: rec.Topic,
					Local:  r.groupID,
				},
				ReleaseFn: rel,
			})
		})
		if anyRecord {
			// 告知客户端此批次处理完成，允许参与重平衡进程。
			cli.AllowRebalance()
		}
	}
}

// Stop 负责该函数对应的核心逻辑，详见实现细节。
func (r *KafkaReceiver) Stop(ctx context.Context) error {
	// Stop 只负责触发 cancel 并等待 Start 退出，不直接关闭 client；
	// client 关闭由 Start defer 统一执行，避免并发 close。
	r.mu.Lock()
	cancel := r.cancel
	done := r.done
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// splitCSV 负责该函数对应的核心逻辑，详见实现细节。
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
