package receiver

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"forward-stub/src/config"
	"forward-stub/src/packet"
)

type skyddsReaderMock struct {
	waits           int32
	drains          int32
	lastWaitTimeout time.Duration
	lastDrainMax    int
	readyCh         chan struct{}
	queued          [][]byte
	closed          atomic.Bool
}

func (m *skyddsReaderMock) Wait(timeout time.Duration) (bool, error) {
	atomic.AddInt32(&m.waits, 1)
	m.lastWaitTimeout = timeout
	select {
	case <-m.readyCh:
		return true, nil
	case <-time.After(timeout):
		if m.closed.Load() {
			return false, nil
		}
		return false, nil
	}
}

func (m *skyddsReaderMock) Drain(maxItems int) ([][]byte, error) {
	atomic.AddInt32(&m.drains, 1)
	m.lastDrainMax = maxItems
	if maxItems <= 0 || len(m.queued) == 0 {
		return nil, nil
	}
	n := maxItems
	if n > len(m.queued) {
		n = len(m.queued)
	}
	out := append([][]byte(nil), m.queued[:n]...)
	m.queued = m.queued[n:]
	return out, nil
}
func (m *skyddsReaderMock) Poll(time.Duration) ([]byte, error) {
	return nil, errors.New("unexpected Poll call")
}
func (m *skyddsReaderMock) PollBatch(time.Duration) ([][]byte, error) {
	return nil, errors.New("unexpected PollBatch call")
}
func (m *skyddsReaderMock) Close() error {
	m.closed.Store(true)
	return nil
}

func TestSkyDDSReceiverBatchSplit(t *testing.T) {
	ready := make(chan struct{}, 1)
	r := &SkyDDSReceiver{
		name:   "rx",
		cfg:    config.ReceiverConfig{DomainID: 0, TopicName: "T"},
		reader: &skyddsReaderMock{readyCh: ready, queued: [][]byte{[]byte("a"), nil, []byte("b")}},
		builder: func() string {
			return "k"
		},
		mode:          "fixed",
		model:         "batch_octet",
		waitTimeout:   5 * time.Millisecond,
		drainMaxItems: 128,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var n int32
	go func() {
		_ = r.Start(ctx, func(p *packet.Packet) {
			atomic.AddInt32(&n, 1)
			p.Release()
		})
	}()
	ready <- struct{}{}
	time.Sleep(100 * time.Millisecond)
	cancel()
	if got := atomic.LoadInt32(&n); got != 2 {
		t.Fatalf("expected 2 split messages, got %d", got)
	}
}

func TestSkyDDSReceiverOctetDrainStillDispatchIndividually(t *testing.T) {
	ready := make(chan struct{}, 1)
	m := &skyddsReaderMock{readyCh: ready, queued: [][]byte{[]byte("x1"), []byte("x2"), []byte("x3")}}
	r := &SkyDDSReceiver{
		name:          "rx",
		cfg:           config.ReceiverConfig{DomainID: 0, TopicName: "T"},
		reader:        m,
		builder:       func() string { return "k" },
		mode:          "fixed",
		model:         "octet",
		waitTimeout:   7 * time.Millisecond,
		drainMaxItems: 3,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var n int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = r.Start(ctx, func(p *packet.Packet) {
			atomic.AddInt32(&n, 1)
			p.Release()
			if atomic.LoadInt32(&n) >= 3 {
				cancel()
			}
		})
	}()
	ready <- struct{}{}
	<-done
	if got := atomic.LoadInt32(&n); got != 3 {
		t.Fatalf("expected 3 packets, got %d", got)
	}
	if got := atomic.LoadInt32(&m.drains); got < 1 {
		t.Fatalf("expected drain called, got %d", got)
	}
	if m.lastWaitTimeout != 7*time.Millisecond {
		t.Fatalf("wait timeout mismatch, got %s", m.lastWaitTimeout)
	}
	if m.lastDrainMax != 3 {
		t.Fatalf("drain max mismatch, got %d", m.lastDrainMax)
	}
}
