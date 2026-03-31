// kafka.go 实现基于 franz-go consumer group 的 Kafka 接收端。
package receiver

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"forward-stub/src/config"
	"forward-stub/src/kafkautil"
	"forward-stub/src/logx"
	"forward-stub/src/packet"

	"github.com/twmb/franz-go/pkg/kgo"
	"go.uber.org/zap/zapcore"
)

type KafkaReceiver struct {
	// name / brokers / topic / groupID 决定该 consumer group 的逻辑身份与复用键。
	name    string
	brokers []string
	topic   string
	groupID string

	// onPacket 是 runtime 注入的 packet 分发回调。
	onPacket func(*packet.Packet)
	// matchKeyBuilder / matchKeyMode 是初始化阶段编译好的 match key 逻辑与模式。
	matchKeyBuilder kafkaMatchKeyBuilder
	matchKeyMode    string

	// mu 保护 client/cancel/done/stats 等运行时状态，避免 Start/Stop 并发释放。
	mu     sync.Mutex
	client *kgo.Client
	cancel context.CancelFunc
	done   chan struct{}

	// stats 是 Kafka receiver 的聚合统计句柄，按 record.Value 字节数累计吞吐。
	stats *logx.TrafficCounter
}

// NewKafkaReceiver 构造 Kafka 接收端，并在冷路径完成各类 consumer 参数解析。
// 这些校验与默认值处理会在服务启动/热重载时提前失败，避免把非法配置带入消费循环。
func NewKafkaReceiver(name string, rc config.ReceiverConfig) (*KafkaReceiver, error) {
	if strings.TrimSpace(rc.Topic) == "" {
		return nil, errors.New("kafka receiver requires topic")
	}
	groupID := strings.TrimSpace(rc.GroupID)
	if groupID == "" {
		groupID = "forward-stub-" + name
	}
	brs := kafkautil.SplitCSV(rc.Listen)
	if len(brs) == 0 {
		return nil, errors.New("kafka receiver requires brokers in listen/remote")
	}
	dialTimeout, err := kafkautil.DurationOrDefault(rc.DialTimeout, config.DefaultKafkaDialTimeout)
	if err != nil {
		return nil, fmt.Errorf("kafka receiver dial_timeout 配置非法: %w", err)
	}
	connIdleTimeout, err := kafkautil.DurationOrDefault(rc.ConnIdleTimeout, config.DefaultKafkaConnIdleTimeout)
	if err != nil {
		return nil, fmt.Errorf("kafka receiver conn_idle_timeout 配置非法: %w", err)
	}
	metadataMaxAge, err := kafkautil.DurationOrDefault(rc.MetadataMaxAge, config.DefaultKafkaMetadataMaxAge)
	if err != nil {
		return nil, fmt.Errorf("kafka receiver metadata_max_age 配置非法: %w", err)
	}
	retryBackoff, err := kafkautil.DurationOrDefault(rc.RetryBackoff, config.DefaultKafkaRetryBackoff)
	if err != nil {
		return nil, fmt.Errorf("kafka receiver retry_backoff 配置非法: %w", err)
	}
	sessionTimeout, err := kafkautil.DurationOrDefault(rc.SessionTimeout, config.DefaultKafkaReceiverSessionTTL)
	if err != nil {
		return nil, fmt.Errorf("kafka receiver session_timeout 配置非法: %w", err)
	}
	heartbeatInterval, err := kafkautil.DurationOrDefault(rc.HeartbeatInterval, config.DefaultKafkaReceiverHeartbeat)
	if err != nil {
		return nil, fmt.Errorf("kafka receiver heartbeat_interval 配置非法: %w", err)
	}
	rebalanceTimeout, err := kafkautil.DurationOrDefault(rc.RebalanceTimeout, config.DefaultKafkaReceiverRebalanceTTL)
	if err != nil {
		return nil, fmt.Errorf("kafka receiver rebalance_timeout 配置非法: %w", err)
	}
	autoCommit := true
	if rc.AutoCommit != nil {
		autoCommit = *rc.AutoCommit
	}
	autoCommitInterval := time.Duration(0)
	if autoCommit {
		autoCommitInterval, err = kafkautil.DurationOrDefault(rc.AutoCommitInterval, config.DefaultKafkaReceiverAutoCommitIv)
		if err != nil {
			return nil, fmt.Errorf("kafka receiver auto_commit_interval 配置非法: %w", err)
		}
	}
	balancers, err := kafkautil.GroupBalancers(rc.Balancers)
	if err != nil {
		return nil, err
	}
	isolationLevel, err := kafkautil.IsolationLevel(rc.IsolationLevel)
	if err != nil {
		return nil, err
	}

	opts := []kgo.Opt{
		kgo.SeedBrokers(brs...),
		kgo.ConsumerGroup(groupID),
		kgo.ConsumeTopics(rc.Topic),
		kgo.DialTimeout(dialTimeout),
		kgo.ConnIdleTimeout(connIdleTimeout),
		kgo.MetadataMaxAge(metadataMaxAge),
		kgo.RetryBackoffFn(func(int) time.Duration { return retryBackoff }),
		kgo.SessionTimeout(sessionTimeout),
		kgo.HeartbeatInterval(heartbeatInterval),
		kgo.RebalanceTimeout(rebalanceTimeout),
		kgo.Balancers(balancers...),
		kgo.FetchMinBytes(int32(kafkautil.IntDefault(rc.FetchMinBytes, 1))),
		kgo.FetchMaxBytes(int32(kafkautil.IntDefault(rc.FetchMaxBytes, 16<<20))),
		kgo.FetchMaxPartitionBytes(int32(kafkautil.IntDefault(rc.FetchMaxPartitionBytes, config.DefaultKafkaFetchMaxPartBytes))),
		kgo.FetchMaxWait(time.Duration(kafkautil.IntDefault(rc.FetchMaxWaitMS, 100)) * time.Millisecond),
		kgo.FetchIsolationLevel(isolationLevel),
	}
	if autoCommit {
		opts = append(opts, kgo.AutoCommitInterval(autoCommitInterval))
	} else {
		opts = append(opts, kgo.DisableAutoCommit())
	}
	if v := strings.TrimSpace(rc.ClientID); v != "" {
		opts = append(opts, kgo.ClientID(v))
	}
	if strings.EqualFold(strings.TrimSpace(rc.StartOffset), "earliest") {
		opts = append(opts, kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()))
	}
	if strings.EqualFold(strings.TrimSpace(rc.StartOffset), "latest") {
		opts = append(opts, kgo.ConsumeResetOffset(kgo.NewOffset().AtEnd()))
	}
	if rc.TLS {
		opts = append(opts, kgo.DialTLSConfig(&tls.Config{InsecureSkipVerify: rc.TLSSkipVerify}))
	}
	if mech, err := kafkautil.BuildSASLMechanism(rc.SASLMechanism, rc.Username, rc.Password); err != nil {
		return nil, err
	} else if mech != nil {
		opts = append(opts, kgo.SASL(mech))
	}
	cli, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, err
	}
	builder, mode, err := compileKafkaMatchKeyBuilder(rc.MatchKey, rc.Topic)
	if err != nil {
		cli.Close()
		return nil, err
	}
	return &KafkaReceiver{
		name:            name,
		brokers:         brs,
		topic:           rc.Topic,
		groupID:         groupID,
		client:          cli,
		matchKeyBuilder: builder,
		matchKeyMode:    mode,
	}, nil
}

// Name 返回 Kafka receiver 的配置名。
func (r *KafkaReceiver) Name() string { return r.name }

// Key 返回 Kafka receiver 的稳定标识。
// 包含 brokers、groupID 和 topic，便于热重载判定实例身份。
func (r *KafkaReceiver) Key() string {
	return "kafka|" + strings.Join(r.brokers, ",") + "|" + r.groupID + "|" + r.topic
}

// MatchKeyMode 返回 Kafka receiver 当前生效的 match key 模式。
func (r *KafkaReceiver) MatchKeyMode() string { return r.matchKeyMode }

// Start 进入 Kafka 拉取循环，并把每条 record 转成 packet。
//
// 聚合统计关系：
//   - Start 时在 info 级别创建 receiver traffic stats；
//   - 每条 record 命中后按 rec.Value 长度累计；
//   - Start 退出时关闭统计句柄与 Kafka client。
func (r *KafkaReceiver) Start(ctx context.Context, onPacket func(*packet.Packet)) error {
	r.onPacket = onPacket

	r.mu.Lock()
	if r.client == nil {
		r.mu.Unlock()
		return errors.New("kafka receiver closed")
	}
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
	cli := r.client
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
			logx.L().Warnw("Kafka接收端拉取失败", "接收端", r.name, "错误", err)
			continue
		}

		var anyRecord bool
		fetches.EachRecord(func(rec *kgo.Record) {
			anyRecord = true
			if r.stats != nil {
				r.stats.AddBytes(len(rec.Value))
			}
			// Kafka record 在进入 runtime 前会复制 payload，避免底层 fetch 缓冲生命周期泄漏到热路径之外。
			payload, rel := packet.CopyFrom(rec.Value)
			matchKey := r.matchKeyBuilder(rec)
			r.onPacket(&packet.Packet{
				Envelope: packet.Envelope{
					Kind:    packet.PayloadKindStream,
					Payload: payload,
					Meta: packet.Meta{
						Proto:    packet.ProtoKafka,
						Remote:   rec.Topic,
						Local:    r.groupID,
						MatchKey: matchKey,
					},
				},
				ReleaseFn: rel,
			})
		})
		if anyRecord {
			// 仅在真正处理过数据后允许 rebalance，可减少长轮询时的无意义抖动。
			cli.AllowRebalance()
		}
	}
}

// Stop 取消消费循环并等待 Start 完成资源回收。
// 超时与否由上层传入的 ctx 控制。
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
