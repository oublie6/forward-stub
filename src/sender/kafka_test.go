package sender

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"forward-stub/src/config"
	"forward-stub/src/kafkautil"
	"forward-stub/src/packet"

	"github.com/twmb/franz-go/pkg/kgo"
)

func TestNewKafkaSenderAppliesBufferedOptions(t *testing.T) {
	s, err := NewKafkaSender("k1", config.SenderConfig{
		Type:               "kafka",
		Remote:             "127.0.0.1:9092",
		Topic:              "out",
		Concurrency:        1,
		MaxBufferedBytes:   2048,
		MaxBufferedRecords: 128,
	})
	if err != nil {
		t.Fatalf("new kafka sender: %v", err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	cli := s.clients[0].Load()
	if cli == nil {
		t.Fatalf("kafka client is nil")
	}
	if got := cli.OptValue(kgo.MaxBufferedBytes).(int64); got != 2048 {
		t.Fatalf("unexpected max buffered bytes: got %d want %d", got, 2048)
	}
	if got := cli.OptValue(kgo.MaxBufferedRecords).(int64); got != 128 {
		t.Fatalf("unexpected max buffered records: got %d want %d", got, 128)
	}
}

func TestNewKafkaSenderKeepsFranzDefaultsForBufferedOptionsWhenUnset(t *testing.T) {
	s, err := NewKafkaSender("k1", config.SenderConfig{
		Type:        "kafka",
		Remote:      "127.0.0.1:9092",
		Topic:       "out",
		Concurrency: 1,
	})
	if err != nil {
		t.Fatalf("new kafka sender: %v", err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	cli := s.clients[0].Load()
	if cli == nil {
		t.Fatalf("kafka client is nil")
	}
	if got := cli.OptValue(kgo.MaxBufferedBytes).(int64); got != 0 {
		t.Fatalf("unexpected default max buffered bytes: got %d want %d", got, 0)
	}
	if got := cli.OptValue(kgo.MaxBufferedRecords).(int64); got != 10000 {
		t.Fatalf("unexpected default max buffered records: got %d want %d", got, 10000)
	}
}

func TestNewKafkaSenderAppliesKafkaTimingAndPartitionerOptions(t *testing.T) {
	s, err := NewKafkaSender("k1", config.SenderConfig{
		Type:            "kafka",
		Remote:          "127.0.0.1:9092",
		Topic:           "out",
		Concurrency:     1,
		DialTimeout:     "11s",
		RequestTimeout:  "31s",
		RetryTimeout:    "2m",
		RetryBackoff:    "300ms",
		ConnIdleTimeout: "45s",
		MetadataMaxAge:  "7m",
		Partitioner:     "round_robin",
	})
	if err != nil {
		t.Fatalf("new kafka sender: %v", err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	cli := s.clients[0].Load()
	if cli == nil {
		t.Fatalf("kafka client is nil")
	}
	if got := cli.OptValue(kgo.DialTimeout).(time.Duration); got != 11*time.Second {
		t.Fatalf("unexpected dial timeout: %s", got)
	}
	if got := cli.OptValue(kgo.ProduceRequestTimeout).(time.Duration); got != 31*time.Second {
		t.Fatalf("unexpected request timeout: %s", got)
	}
	if got := cli.OptValue(kgo.RetryTimeout).(time.Duration); got != 2*time.Minute {
		t.Fatalf("unexpected retry timeout: %s", got)
	}
	backoffFn := cli.OptValue(kgo.RetryBackoffFn).(func(int) time.Duration)
	if got := backoffFn(3); got != 300*time.Millisecond {
		t.Fatalf("unexpected retry backoff: %s", got)
	}
	if got := cli.OptValue(kgo.ConnIdleTimeout).(time.Duration); got != 45*time.Second {
		t.Fatalf("unexpected conn idle timeout: %s", got)
	}
	if got := cli.OptValue(kgo.MetadataMaxAge).(time.Duration); got != 7*time.Minute {
		t.Fatalf("unexpected metadata max age: %s", got)
	}
	if got := reflect.TypeOf(cli.OptValue(kgo.RecordPartitioner)).String(); !strings.Contains(got, "roundRobinPartitioner") {
		t.Fatalf("unexpected partitioner type: %s", got)
	}
}

func TestKafkaRecordKeyUsesConfiguredSource(t *testing.T) {
	pkt := &packet.Packet{
		Envelope: packet.Envelope{
			Payload: []byte("payload"),
			Meta: packet.Meta{
				MatchKey:       "kafka|topic=in|partition=0",
				KafkaRecordKey: "orders-42",
				Remote:         "remote",
				Local:          "local",
				FileName:       "a.txt",
				FilePath:       "/tmp/a.txt",
				TransferID:     "tx-1",
				RouteSender:    "tx_kafka",
			},
		},
	}
	if got := string(kafkautil.RecordKey([]byte("fixed"), "", pkt)); got != "fixed" {
		t.Fatalf("unexpected fixed key: %q", got)
	}
	if got := string(kafkautil.RecordKey(nil, "match_key", pkt)); got != pkt.Meta.MatchKey {
		t.Fatalf("unexpected match_key source: %q", got)
	}
	if got := string(kafkautil.RecordKey(nil, "kafka_record_key", pkt)); got != pkt.Meta.KafkaRecordKey {
		t.Fatalf("unexpected kafka_record_key source: %q", got)
	}
	if got := string(kafkautil.RecordKey(nil, "payload", pkt)); got != "payload" {
		t.Fatalf("unexpected payload source: %q", got)
	}
}

func TestKafkaCompressionCodecAppliesLevel(t *testing.T) {
	codec, enabled, err := kafkautil.CompressionCodec("zstd", 7)
	if err != nil {
		t.Fatalf("compression codec: %v", err)
	}
	if !enabled {
		t.Fatal("expected compression enabled")
	}
	if compressor, err := kgo.DefaultCompressor(codec); err != nil || compressor == nil {
		t.Fatalf("expected valid compressor, err=%v", err)
	}
}

func TestKafkaSenderDefaultModeIsAsync(t *testing.T) {
	s, err := NewKafkaSender("k1", config.SenderConfig{
		Type:        "kafka",
		Remote:      "127.0.0.1:9092",
		Topic:       "out",
		Concurrency: 1,
	})
	if err != nil {
		t.Fatalf("new kafka sender: %v", err)
	}
	defer func() { _ = s.Close(context.Background()) }()

	if s.sendMode != "async" {
		t.Fatalf("send mode=%q want async", s.sendMode)
	}
	if s.asyncQueueSize != config.DefaultKafkaSenderAsyncQueueSize {
		t.Fatalf("async queue size=%d want %d", s.asyncQueueSize, config.DefaultKafkaSenderAsyncQueueSize)
	}
}

func TestKafkaSenderAsyncSendEnqueuesAndCallbackStats(t *testing.T) {
	s := newTestKafkaAsyncSender(1, 4)
	s.startTestWorkers()
	defer func() { _ = s.Close(context.Background()) }()

	produced := make(chan struct{}, 1)
	s.produceAsyncFn = func(_ context.Context, _ int, rec *kgo.Record, cb func(error)) {
		if string(rec.Value) != "hello" {
			t.Errorf("record value=%q want hello", rec.Value)
		}
		cb(nil)
		produced <- struct{}{}
	}

	if err := s.Send(context.Background(), &packet.Packet{Envelope: packet.Envelope{Payload: []byte("hello")}}); err != nil {
		t.Fatalf("async send enqueue: %v", err)
	}
	select {
	case <-produced:
	case <-time.After(time.Second):
		t.Fatal("produce callback not observed")
	}
	stats := s.AsyncRuntimeStats()
	if stats.SendSuccess != 1 || stats.SendErrors != 0 || stats.Dropped != 0 {
		t.Fatalf("unexpected async stats: %+v", stats)
	}
}

func TestKafkaSenderAsyncCallbackErrorStats(t *testing.T) {
	s := newTestKafkaAsyncSender(1, 4)
	s.startTestWorkers()
	defer func() { _ = s.Close(context.Background()) }()

	produced := make(chan struct{}, 1)
	s.produceAsyncFn = func(_ context.Context, _ int, _ *kgo.Record, cb func(error)) {
		cb(errors.New("broker rejected"))
		produced <- struct{}{}
	}
	if err := s.Send(context.Background(), &packet.Packet{Envelope: packet.Envelope{Payload: []byte("hello")}}); err != nil {
		t.Fatalf("async send enqueue: %v", err)
	}
	select {
	case <-produced:
	case <-time.After(time.Second):
		t.Fatal("produce callback not observed")
	}
	stats := s.AsyncRuntimeStats()
	if stats.SendErrors != 1 || stats.SendSuccess != 0 {
		t.Fatalf("unexpected async error stats: %+v", stats)
	}
}

func TestKafkaSenderSyncModeUsesProduceSync(t *testing.T) {
	s := &KafkaSender{
		name:        "k1",
		topic:       "out",
		concurrency: 1,
		clients:     make([]atomic.Pointer[kgo.Client], 1),
		sendMode:    "sync",
	}
	var calls atomic.Int64
	s.produceSyncFn = func(_ context.Context, _ int, rec *kgo.Record) error {
		calls.Add(1)
		if string(rec.Value) != "sync" {
			t.Errorf("record value=%q want sync", rec.Value)
		}
		return nil
	}
	if err := s.Send(context.Background(), &packet.Packet{Envelope: packet.Envelope{Payload: []byte("sync")}}); err != nil {
		t.Fatalf("sync send: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("produce sync calls=%d want 1", calls.Load())
	}

	wantErr := errors.New("boom")
	s.produceSyncFn = func(context.Context, int, *kgo.Record) error { return wantErr }
	if err := s.Send(context.Background(), &packet.Packet{Envelope: packet.Envelope{Payload: []byte("sync")}}); !errors.Is(err, wantErr) {
		t.Fatalf("sync send err=%v want %v", err, wantErr)
	}
}

func TestKafkaSenderAsyncBackpressureErrorWhenQueueFull(t *testing.T) {
	s := newTestKafkaAsyncSender(1, 1)
	s.asyncBackpressure = "error"
	s.queues[0] <- kafkaAsyncItem{record: &kgo.Record{Topic: "out"}}

	err := s.Send(context.Background(), &packet.Packet{Envelope: packet.Envelope{Payload: []byte("x")}})
	if err == nil || !strings.Contains(err.Error(), "queue full") {
		t.Fatalf("send err=%v want queue full", err)
	}
	if got := s.AsyncRuntimeStats().Dropped; got != 1 {
		t.Fatalf("dropped=%d want 1", got)
	}
}

func TestKafkaSenderAsyncBackpressureBlockHonorsContext(t *testing.T) {
	s := newTestKafkaAsyncSender(1, 1)
	s.queues[0] <- kafkaAsyncItem{record: &kgo.Record{Topic: "out"}}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := s.Send(ctx, &packet.Packet{Envelope: packet.Envelope{Payload: []byte("x")}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("send err=%v want deadline exceeded", err)
	}
	if got := s.AsyncRuntimeStats().Dropped; got != 1 {
		t.Fatalf("dropped=%d want 1", got)
	}
}

func TestKafkaSenderAsyncCloseDrainsAndWaitsCallbacks(t *testing.T) {
	s := newTestKafkaAsyncSender(1, 8)
	s.startTestWorkers()
	var flushCalls atomic.Int64
	var closeCalls atomic.Int64
	var clientClosed atomic.Bool
	var mu sync.Mutex
	callbacks := make([]func(error), 0, 3)
	produced := make(chan struct{}, 3)
	closeReturned := make(chan struct{})

	s.produceAsyncFn = func(_ context.Context, _ int, _ *kgo.Record, cb func(error)) {
		mu.Lock()
		callbacks = append(callbacks, cb)
		mu.Unlock()
		produced <- struct{}{}
	}
	s.flushFn = func(context.Context, int) error {
		flushCalls.Add(1)
		return nil
	}
	s.closeClientFn = func(int) {
		if clientClosed.CompareAndSwap(false, true) {
			closeCalls.Add(1)
		}
	}

	for i := 0; i < 3; i++ {
		if err := s.Send(context.Background(), &packet.Packet{Envelope: packet.Envelope{Payload: []byte("x")}}); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}

	go func() {
		_ = s.Close(context.Background())
		close(closeReturned)
	}()

	for i := 0; i < 3; i++ {
		select {
		case <-produced:
		case <-time.After(time.Second):
			t.Fatalf("record %d not drained", i)
		}
	}
	select {
	case <-closeReturned:
		t.Fatal("close returned before callbacks completed")
	case <-time.After(20 * time.Millisecond):
	}

	mu.Lock()
	for _, cb := range callbacks {
		cb(nil)
	}
	mu.Unlock()

	select {
	case <-closeReturned:
	case <-time.After(time.Second):
		t.Fatal("close did not return after callbacks")
	}
	if flushCalls.Load() != 1 || closeCalls.Load() != 1 {
		t.Fatalf("flush calls=%d close calls=%d want 1/1", flushCalls.Load(), closeCalls.Load())
	}
	if stats := s.AsyncRuntimeStats(); stats.SendSuccess != 3 || stats.SendErrors != 0 {
		t.Fatalf("unexpected async stats after close: %+v", stats)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if closeCalls.Load() != 1 {
		t.Fatalf("repeat close should be idempotent, got close calls %d", closeCalls.Load())
	}
}

func newTestKafkaAsyncSender(concurrency, queueSize int) *KafkaSender {
	s := &KafkaSender{
		name:              "k1",
		topic:             "out",
		concurrency:       concurrency,
		shardMask:         concurrency - 1,
		clients:           make([]atomic.Pointer[kgo.Client], concurrency),
		sendMode:          "async",
		asyncQueueSize:    queueSize,
		asyncBackpressure: "block",
		closeFlushTimeout: time.Second,
		queues:            make([]chan kafkaAsyncItem, concurrency),
		closeCh:           make(chan struct{}),
	}
	s.produceAsyncFn = func(_ context.Context, _ int, _ *kgo.Record, cb func(error)) { cb(nil) }
	for i := 0; i < concurrency; i++ {
		s.queues[i] = make(chan kafkaAsyncItem, queueSize)
	}
	return s
}

func (s *KafkaSender) startTestWorkers() {
	for i := 0; i < s.concurrency; i++ {
		s.workers.Add(1)
		go s.asyncWorker(i, s.queues[i])
	}
}
