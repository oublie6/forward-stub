// kafka.go 实现基于 franz-go 的 Kafka 发送端。
package sender

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"sync"
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

	sendMode          string
	asyncQueueSize    int
	asyncBackpressure string
	closeFlushTimeout time.Duration

	queues    []chan kafkaAsyncItem
	closeCh   chan struct{}
	closeOnce sync.Once
	closed    atomic.Bool
	workers   sync.WaitGroup
	inflight  sync.WaitGroup

	dropped     atomic.Uint64
	sendErrors  atomic.Uint64
	sendSuccess atomic.Uint64

	produceSyncFn  func(context.Context, int, *kgo.Record) error
	produceAsyncFn func(context.Context, int, *kgo.Record, func(error))
	flushFn        func(context.Context, int) error
	closeClientFn  func(int)

	unregisterRuntimeStats func()
}

type kafkaAsyncItem struct {
	record *kgo.Record
}

// NewKafkaSender 创建 franz-go producer 分片。
// 所有 Kafka 选项在构造阶段编译完成；sync 模式热路径直接 ProduceSync，
// async 模式热路径只入本地有界队列，broker ack 由 callback 统计。
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
	closeFlushTimeout, err := kafkautil.DurationOrDefault(sc.CloseFlushTimeout, config.DefaultKafkaSenderCloseFlushTTL)
	if err != nil {
		return nil, fmt.Errorf("kafka sender close_flush_timeout 配置非法: %w", err)
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
	sendMode := strings.TrimSpace(sc.SendMode)
	if sendMode == "" {
		sendMode = config.DefaultKafkaSenderSendMode
	}
	asyncQueueSize := kafkautil.IntDefault(sc.AsyncQueueSize, config.DefaultKafkaSenderAsyncQueueSize)
	asyncBackpressure := strings.TrimSpace(sc.AsyncBackpressure)
	if asyncBackpressure == "" {
		asyncBackpressure = config.DefaultKafkaSenderBackpressure
	}
	s := &KafkaSender{
		name:              name,
		brokers:           brs,
		topic:             sc.Topic,
		concurrency:       concurrency,
		shardMask:         concurrency - 1,
		recordKey:         []byte(sc.RecordKey),
		keySource:         strings.TrimSpace(sc.RecordKeySource),
		clients:           make([]atomic.Pointer[kgo.Client], concurrency),
		sendMode:          sendMode,
		asyncQueueSize:    asyncQueueSize,
		asyncBackpressure: asyncBackpressure,
		closeFlushTimeout: closeFlushTimeout,
	}
	if sendMode == "async" {
		s.closeCh = make(chan struct{})
		s.queues = make([]chan kafkaAsyncItem, concurrency)
	}
	for i := 0; i < concurrency; i++ {
		cli, err := kgo.NewClient(opts...)
		if err != nil {
			_ = s.Close(context.Background())
			return nil, err
		}
		s.clients[i].Store(cli)
		if sendMode == "async" {
			s.queues[i] = make(chan kafkaAsyncItem, asyncQueueSize)
			s.workers.Add(1)
			go s.asyncWorker(i, s.queues[i])
		}
	}
	if sendMode == "async" {
		s.unregisterRuntimeStats = logx.RegisterSenderRuntimeStats(s.name, s.senderRuntimeStats)
	}
	return s, nil
}

// Name 返回 sender 配置名。
func (s *KafkaSender) Name() string { return s.name }

// Key 返回 Kafka broker+topic 身份键。
func (s *KafkaSender) Key() string { return "kafka|" + strings.Join(s.brokers, ",") + "|" + s.topic }

// Send 生产一条 Kafka record。
// sync 模式下 nil 表示 ProduceSync 已成功；async 模式下 nil 仅表示已进入本地队列，
// 真正发送成功/失败会在 franz-go callback 中进入 send_success/send_errors 统计。
// record key 来源已在配置校验阶段约束，运行时只按预编译字段取值。
func (s *KafkaSender) Send(ctx context.Context, p *packet.Packet) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if s.closed.Load() {
		return errors.New("kafka sender closed")
	}
	idx := nextShardIndex(&s.nextIdx, s.shardMask)
	if idx < 0 || idx >= len(s.clients) {
		return errors.New("kafka sender closed")
	}
	cli := s.clients[idx].Load()
	rec := &kgo.Record{Topic: s.topic, Value: p.Payload, Key: kafkautil.RecordKey(s.recordKey, s.keySource, p)}

	if s.sendMode == "async" {
		if cli == nil && s.produceAsyncFn == nil {
			return errors.New("kafka sender closed")
		}
		return s.enqueueAsync(ctx, idx, rec)
	}

	if cli == nil && s.produceSyncFn == nil {
		return errors.New("kafka sender closed")
	}
	if err := s.produceSync(ctx, idx, rec); err != nil {
		logx.L().Warnw("Kafka发送失败", "发送端", s.name, "主题", s.topic, "错误", err)
		return err
	}
	return nil
}

// Close 关闭所有 producer 分片；允许重复调用。
// async 模式会先停止接收新消息，再尽力 drain 本地队列、等待 callback 和 Flush；
// 整个等待受 close_flush_timeout 与传入 ctx 共同约束，避免停机无限阻塞。
func (s *KafkaSender) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if s.sendMode == "async" {
		if s.unregisterRuntimeStats != nil {
			s.unregisterRuntimeStats()
		}
		s.closeOnce.Do(func() {
			s.closed.Store(true)
			if s.closeCh != nil {
				close(s.closeCh)
			}
		})

		waitCtx := ctx
		cancel := func() {}
		if s.closeFlushTimeout > 0 {
			waitCtx, cancel = context.WithTimeout(ctx, s.closeFlushTimeout)
		}
		defer cancel()

		if err := waitGroupDone(waitCtx, &s.workers); err != nil {
			logx.L().Warnw("Kafka异步发送队列关闭等待超时", "发送端", s.name, "错误", err)
		}
		for i := 0; i < s.concurrency; i++ {
			if err := s.flush(waitCtx, i); err != nil {
				logx.L().Warnw("Kafka异步发送Flush失败", "发送端", s.name, "分片", i, "错误", err)
			}
		}
		if err := waitGroupDone(waitCtx, &s.inflight); err != nil {
			logx.L().Warnw("Kafka异步发送callback等待超时", "发送端", s.name, "错误", err)
		}
	} else {
		s.closed.Store(true)
	}
	for i := 0; i < s.concurrency; i++ {
		s.closeClient(i)
	}
	return nil
}

func (s *KafkaSender) enqueueAsync(ctx context.Context, idx int, rec *kgo.Record) error {
	if idx < 0 || idx >= len(s.queues) || s.queues[idx] == nil {
		return errors.New("kafka async queue not initialized")
	}
	item := kafkaAsyncItem{record: rec}
	switch s.asyncBackpressure {
	case "error":
		select {
		case <-s.closeCh:
			return errors.New("kafka sender closed")
		case s.queues[idx] <- item:
			return nil
		default:
			s.dropped.Add(1)
			return errors.New("kafka async queue full")
		}
	default:
		select {
		case <-s.closeCh:
			return errors.New("kafka sender closed")
		case s.queues[idx] <- item:
			return nil
		case <-ctx.Done():
			s.dropped.Add(1)
			return ctx.Err()
		}
	}
}

func (s *KafkaSender) asyncWorker(idx int, q <-chan kafkaAsyncItem) {
	defer s.workers.Done()
	for {
		select {
		case item := <-q:
			s.produceAsyncItem(idx, item)
			continue
		default:
		}

		select {
		case item := <-q:
			s.produceAsyncItem(idx, item)
		case <-s.closeCh:
			for {
				select {
				case item := <-q:
					s.produceAsyncItem(idx, item)
				default:
					return
				}
			}
		}
	}
}

func (s *KafkaSender) produceAsyncItem(idx int, item kafkaAsyncItem) {
	s.inflight.Add(1)
	s.produceAsync(context.Background(), idx, item.record, func(err error) {
		defer s.inflight.Done()
		if err != nil {
			s.sendErrors.Add(1)
			logx.L().Warnw("Kafka异步发送失败", "发送端", s.name, "主题", s.topic, "分片", idx, "错误", err)
			return
		}
		s.sendSuccess.Add(1)
	})
}

func (s *KafkaSender) produceSync(ctx context.Context, idx int, rec *kgo.Record) error {
	if s.produceSyncFn != nil {
		return s.produceSyncFn(ctx, idx, rec)
	}
	cli := s.clients[idx].Load()
	if cli == nil {
		return errors.New("kafka sender closed")
	}
	return cli.ProduceSync(ctx, rec).FirstErr()
}

func (s *KafkaSender) produceAsync(ctx context.Context, idx int, rec *kgo.Record, cb func(error)) {
	if s.produceAsyncFn != nil {
		s.produceAsyncFn(ctx, idx, rec, cb)
		return
	}
	cli := s.clients[idx].Load()
	if cli == nil {
		cb(errors.New("kafka sender closed"))
		return
	}
	cli.Produce(ctx, rec, func(_ *kgo.Record, err error) { cb(err) })
}

func (s *KafkaSender) flush(ctx context.Context, idx int) error {
	if s.flushFn != nil {
		return s.flushFn(ctx, idx)
	}
	cli := s.clients[idx].Load()
	if cli == nil {
		return nil
	}
	return cli.Flush(ctx)
}

func (s *KafkaSender) closeClient(idx int) {
	if s.closeClientFn != nil {
		s.closeClientFn(idx)
		return
	}
	cli := s.clients[idx].Swap(nil)
	if cli != nil {
		cli.Close()
	}
}

func waitGroupDone(ctx context.Context, wg *sync.WaitGroup) error {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *KafkaSender) AsyncRuntimeStats() AsyncRuntimeStats {
	if s.sendMode != "async" {
		return AsyncRuntimeStats{}
	}
	stats := AsyncRuntimeStats{
		Dropped:     s.dropped.Load(),
		SendErrors:  s.sendErrors.Load(),
		SendSuccess: s.sendSuccess.Load(),
	}
	for _, q := range s.queues {
		if q == nil {
			continue
		}
		stats.QueueSize += cap(q)
		stats.QueueUsed += len(q)
	}
	stats.QueueAvailable = stats.QueueSize - stats.QueueUsed
	return stats
}

func (s *KafkaSender) senderRuntimeStats() logx.SenderRuntimeStats {
	stats := s.AsyncRuntimeStats()
	return logx.SenderRuntimeStats{
		QueueSize:      stats.QueueSize,
		QueueUsed:      stats.QueueUsed,
		QueueAvailable: stats.QueueAvailable,
		Dropped:        stats.Dropped,
		SendErrors:     stats.SendErrors,
		SendSuccess:    stats.SendSuccess,
	}
}
