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
	"forward-stub/src/logx"
	"forward-stub/src/packet"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
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

// NewKafkaReceiver 负责该函数对应的核心逻辑，详见实现细节。
func NewKafkaReceiver(name string, rc config.ReceiverConfig) (*KafkaReceiver, error) {
	if strings.TrimSpace(rc.Topic) == "" {
		return nil, errors.New("kafka receiver requires topic")
	}
	groupID := strings.TrimSpace(rc.GroupID)
	if groupID == "" {
		groupID = "forward-stub-" + name
	}
	brs := splitCSV(rc.Listen)
	if len(brs) == 0 {
		return nil, errors.New("kafka receiver requires brokers in listen/remote")
	}
	dialTimeout, err := kafkaDurationOrDefault(rc.DialTimeout, config.DefaultKafkaDialTimeout)
	if err != nil {
		return nil, fmt.Errorf("kafka receiver dial_timeout 配置非法: %w", err)
	}
	connIdleTimeout, err := kafkaDurationOrDefault(rc.ConnIdleTimeout, config.DefaultKafkaConnIdleTimeout)
	if err != nil {
		return nil, fmt.Errorf("kafka receiver conn_idle_timeout 配置非法: %w", err)
	}
	metadataMaxAge, err := kafkaDurationOrDefault(rc.MetadataMaxAge, config.DefaultKafkaMetadataMaxAge)
	if err != nil {
		return nil, fmt.Errorf("kafka receiver metadata_max_age 配置非法: %w", err)
	}
	retryBackoff, err := kafkaDurationOrDefault(rc.RetryBackoff, config.DefaultKafkaRetryBackoff)
	if err != nil {
		return nil, fmt.Errorf("kafka receiver retry_backoff 配置非法: %w", err)
	}
	sessionTimeout, err := kafkaDurationOrDefault(rc.SessionTimeout, config.DefaultKafkaReceiverSessionTTL)
	if err != nil {
		return nil, fmt.Errorf("kafka receiver session_timeout 配置非法: %w", err)
	}
	heartbeatInterval, err := kafkaDurationOrDefault(rc.HeartbeatInterval, config.DefaultKafkaReceiverHeartbeat)
	if err != nil {
		return nil, fmt.Errorf("kafka receiver heartbeat_interval 配置非法: %w", err)
	}
	rebalanceTimeout, err := kafkaDurationOrDefault(rc.RebalanceTimeout, config.DefaultKafkaReceiverRebalanceTTL)
	if err != nil {
		return nil, fmt.Errorf("kafka receiver rebalance_timeout 配置非法: %w", err)
	}
	autoCommit := true
	if rc.AutoCommit != nil {
		autoCommit = *rc.AutoCommit
	}
	autoCommitInterval := time.Duration(0)
	if autoCommit {
		autoCommitInterval, err = kafkaDurationOrDefault(rc.AutoCommitInterval, config.DefaultKafkaReceiverAutoCommitIv)
		if err != nil {
			return nil, fmt.Errorf("kafka receiver auto_commit_interval 配置非法: %w", err)
		}
	}
	balancers, err := kafkaGroupBalancers(rc.Balancers)
	if err != nil {
		return nil, err
	}
	isolationLevel, err := kafkaIsolationLevel(rc.IsolationLevel)
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
		kgo.FetchMinBytes(int32(kafkaIntDefault(rc.FetchMinBytes, 1))),
		kgo.FetchMaxBytes(int32(kafkaIntDefault(rc.FetchMaxBytes, 16<<20))),
		kgo.FetchMaxPartitionBytes(int32(kafkaIntDefault(rc.FetchMaxPartitionBytes, config.DefaultKafkaFetchMaxPartBytes))),
		kgo.FetchMaxWait(time.Duration(kafkaIntDefault(rc.FetchMaxWaitMS, 100)) * time.Millisecond),
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
	if mech, err := buildKafkaSASLMechanism(rc.SASLMechanism, rc.Username, rc.Password); err != nil {
		return nil, err
	} else if mech != nil {
		opts = append(opts, kgo.SASL(mech))
	}
	cli, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, err
	}
	return &KafkaReceiver{name: name, brokers: brs, topic: rc.Topic, groupID: groupID, client: cli}, nil
}

func (r *KafkaReceiver) Name() string { return r.name }

func (r *KafkaReceiver) Key() string {
	return "kafka|" + strings.Join(r.brokers, ",") + "|" + r.groupID + "|" + r.topic
}

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
			payload, rel := packet.CopyFrom(rec.Value)
			matchKey := BuildMatchKey(
				"kafka",
				MatchKeyField{Name: "topic", Value: rec.Topic},
				MatchKeyField{Name: "partition", Value: fmt.Sprintf("%d", rec.Partition)},
			)
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

func kafkaIntDefault(v, d int) int {
	if v <= 0 {
		return d
	}
	return v
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

func kafkaIsolationLevel(v string) (kgo.IsolationLevel, error) {
	switch strings.TrimSpace(v) {
	case "", "read_uncommitted":
		return kgo.ReadUncommitted(), nil
	case "read_committed":
		return kgo.ReadCommitted(), nil
	default:
		return kgo.ReadUncommitted(), fmt.Errorf("kafka isolation_level %s unsupported", v)
	}
}

func kafkaGroupBalancers(values []string) ([]kgo.GroupBalancer, error) {
	if len(values) == 0 {
		values = config.DefaultKafkaReceiverBalancers
	}
	out := make([]kgo.GroupBalancer, 0, len(values))
	for _, value := range values {
		switch strings.TrimSpace(value) {
		case "range":
			out = append(out, kgo.RangeBalancer())
		case "round_robin":
			out = append(out, kgo.RoundRobinBalancer())
		case "cooperative_sticky":
			out = append(out, kgo.CooperativeStickyBalancer())
		default:
			return nil, fmt.Errorf("kafka balancer %s unsupported", value)
		}
	}
	return out, nil
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
