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
	name    string
	brokers []string
	topic   string
	groupID string

	onPacket func(*packet.Packet)

	mu     sync.Mutex
	client *kgo.Client
	cancel context.CancelFunc
	done   chan struct{}

	stats *logx.TrafficCounter
}

func NewKafkaReceiver(name, brokers, topic, groupID string) (*KafkaReceiver, error) {
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

func (r *KafkaReceiver) Name() string { return r.name }
func (r *KafkaReceiver) Key() string {
	return "kafka|" + strings.Join(r.brokers, ",") + "|" + r.groupID + "|" + r.topic
}

func (r *KafkaReceiver) Start(ctx context.Context, onPacket func(*packet.Packet)) error {
	r.onPacket = onPacket

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
			cli.AllowRebalance()
		}
	}
}

func (r *KafkaReceiver) Stop(ctx context.Context) error {
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
