package receiver

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"forward-stub/src/config"
	"forward-stub/src/packet"
	"forward-stub/src/skydds"
)

type scriptedSkyDDSReaderMock struct {
	mu sync.Mutex

	waits           int32
	drains          int32
	lastWaitTimeout time.Duration
	lastDrainMax    int

	// waitQueue 决定每次 Wait 的返回值；为空时返回 false,nil。
	waitQueue []bool
	// drainQueue 按次序返回 Drain 结果；为空时返回 nil,nil。
	drainQueue [][][]byte
	closed     atomic.Bool
}

func (m *scriptedSkyDDSReaderMock) Wait(timeout time.Duration) (bool, error) {
	atomic.AddInt32(&m.waits, 1)
	m.lastWaitTimeout = timeout
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.waitQueue) == 0 {
		return false, nil
	}
	v := m.waitQueue[0]
	m.waitQueue = m.waitQueue[1:]
	return v, nil
}

func (m *scriptedSkyDDSReaderMock) Drain(maxItems int) ([][]byte, error) {
	atomic.AddInt32(&m.drains, 1)
	m.lastDrainMax = maxItems
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.drainQueue) == 0 {
		return nil, nil
	}
	out := m.drainQueue[0]
	m.drainQueue = m.drainQueue[1:]
	return out, nil
}

func (m *scriptedSkyDDSReaderMock) Poll(time.Duration) ([]byte, error) {
	return nil, errors.New("unexpected Poll call")
}
func (m *scriptedSkyDDSReaderMock) PollBatch(time.Duration) ([][]byte, error) {
	return nil, errors.New("unexpected PollBatch call")
}
func (m *scriptedSkyDDSReaderMock) Close() error {
	m.closed.Store(true)
	return nil
}

func TestNewSkyDDSReceiverPassesCommonOptionsAndKnobs(t *testing.T) {
	orig := skyddsReaderFactory
	defer func() { skyddsReaderFactory = orig }()

	var got skydds.CommonOptions
	skyddsReaderFactory = func(opts skydds.CommonOptions) (skydds.Reader, error) {
		got = opts
		return &scriptedSkyDDSReaderMock{}, nil
	}

	cfg := config.ReceiverConfig{
		Type:                "dds_skydds",
		Selector:            "sel",
		DCPSConfigFile:      "/tmp/dds.ini",
		DomainID:            1,
		TopicName:           "topicA",
		MessageModel:        "octet",
		Reliable:            true,
		QueueDepth:          256,
		MaxBlockingTimeMsec: 75,
		ConsumerGroup:       "group-a",
		Compress:            true,
		WaitTimeout:         "9ms",
		DrainMaxItems:       11,
		DrainBufferBytes:    123456,
	}
	r, err := NewSkyDDSReceiver("rx", cfg)
	if err != nil {
		t.Fatalf("new receiver: %v", err)
	}
	if got.DCPSConfigFile != cfg.DCPSConfigFile || got.DomainID != cfg.DomainID || got.TopicName != cfg.TopicName || got.MessageModel != cfg.MessageModel {
		t.Fatalf("unexpected reader options: %+v", got)
	}
	if r.waitTimeout != 9*time.Millisecond {
		t.Fatalf("wait timeout mismatch: %s", r.waitTimeout)
	}
	if r.drainMaxItems != 11 {
		t.Fatalf("drain max mismatch: %d", r.drainMaxItems)
	}
	if got.DrainBufferBytes != cfg.DrainBufferBytes {
		t.Fatalf("drain buffer bytes mismatch: got=%d want=%d", got.DrainBufferBytes, cfg.DrainBufferBytes)
	}
	if !got.Reliable || got.QueueDepth != 256 || got.MaxBlockingTimeMsec != 75 || got.ConsumerGroup != "group-a" || !got.Compress {
		t.Fatalf("unexpected reader qos/group options: %+v", got)
	}
}

func TestSkyDDSReceiverBatchSplitAndPerPacketDispatch(t *testing.T) {
	m := &scriptedSkyDDSReaderMock{
		waitQueue: []bool{true, false},
		drainQueue: [][][]byte{
			{[]byte("a"), nil, []byte("b")},
			nil,
		},
	}
	r := &SkyDDSReceiver{
		name:          "rx",
		cfg:           config.ReceiverConfig{DomainID: 0, TopicName: "T"},
		reader:        m,
		builder:       func() string { return "k" },
		mode:          "fixed",
		model:         "batch_octet",
		waitTimeout:   5 * time.Millisecond,
		drainMaxItems: 4,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var got [][]byte
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = r.Start(ctx, func(p *packet.Packet) {
			got = append(got, append([]byte(nil), p.Payload...))
			p.Release()
			if len(got) == 2 {
				cancel()
			}
		})
	}()
	<-done
	if len(got) != 2 || string(got[0]) != "a" || string(got[1]) != "b" {
		t.Fatalf("per-packet dispatch mismatch: %+v", got)
	}
}

func TestSkyDDSReceiverOctetDrainRespectsMaxItemsAcrossRounds(t *testing.T) {
	m := &scriptedSkyDDSReaderMock{
		waitQueue: []bool{true, false},
		drainQueue: [][][]byte{
			{[]byte("m1"), []byte("m2")}, // round 1
			{[]byte("m3"), []byte("m4")}, // round 2
			{[]byte("m5")},               // round 3
			nil,                          // stop draining
		},
	}
	r := &SkyDDSReceiver{
		name:          "rx",
		cfg:           config.ReceiverConfig{DomainID: 0, TopicName: "T"},
		reader:        m,
		builder:       func() string { return "k" },
		mode:          "fixed",
		model:         "octet",
		waitTimeout:   7 * time.Millisecond,
		drainMaxItems: 2,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got := make([]string, 0, 5)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = r.Start(ctx, func(p *packet.Packet) {
			got = append(got, string(p.Payload))
			p.Release()
			if len(got) == 5 {
				cancel()
			}
		})
	}()
	<-done

	want := []string{"m1", "m2", "m3", "m4", "m5"}
	if len(got) != len(want) {
		t.Fatalf("packet count mismatch: got=%d want=%d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("packet order mismatch at %d: got=%s want=%s", i, got[i], want[i])
		}
	}
	if m.lastWaitTimeout != 7*time.Millisecond {
		t.Fatalf("wait timeout mismatch, got %s", m.lastWaitTimeout)
	}
	if m.lastDrainMax != 2 {
		t.Fatalf("drain max mismatch, got %d", m.lastDrainMax)
	}
	if gotDrains := atomic.LoadInt32(&m.drains); gotDrains < 4 {
		t.Fatalf("expected multi-round drain calls, got %d", gotDrains)
	}
}

func TestSkyDDSReceiverStopClosesReader(t *testing.T) {
	m := &scriptedSkyDDSReaderMock{}
	r := &SkyDDSReceiver{reader: m}
	if err := r.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if !m.closed.Load() {
		t.Fatal("expected reader close called")
	}
}
