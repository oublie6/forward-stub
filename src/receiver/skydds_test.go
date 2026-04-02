package receiver

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"forward-stub/src/config"
	"forward-stub/src/packet"
)

type skyddsReaderMock struct {
	polls      int32
	batchPolls int32
	batch      [][]byte
}

func (m *skyddsReaderMock) Poll(time.Duration) ([]byte, error) {
	if atomic.AddInt32(&m.polls, 1) == 1 {
		return []byte("x"), nil
	}
	return nil, nil
}
func (m *skyddsReaderMock) PollBatch(time.Duration) ([][]byte, error) {
	if atomic.AddInt32(&m.batchPolls, 1) == 1 {
		return m.batch, nil
	}
	return nil, nil
}
func (m *skyddsReaderMock) Close() error { return nil }

func TestSkyDDSReceiverBatchSplit(t *testing.T) {
	r := &SkyDDSReceiver{
		name:   "rx",
		cfg:    config.ReceiverConfig{DomainID: 0, TopicName: "T"},
		reader: &skyddsReaderMock{batch: [][]byte{[]byte("a"), nil, []byte("b")}},
		builder: func() string {
			return "k"
		},
		mode:  "fixed",
		model: "batch_octet",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var n int32
	go func() {
		_ = r.Start(ctx, func(p *packet.Packet) {
			atomic.AddInt32(&n, 1)
			p.Release()
			if atomic.LoadInt32(&n) >= 2 {
				cancel()
			}
		})
	}()
	time.Sleep(120 * time.Millisecond)
	if got := atomic.LoadInt32(&n); got != 2 {
		t.Fatalf("expected 2 split messages, got %d", got)
	}
}
