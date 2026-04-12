package task

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"forward-stub/src/packet"
	"forward-stub/src/sender"
)

type gateSender struct {
	testNamedSender

	entered chan struct{}
	release chan struct{}
}

var _ sender.Sender = (*gateSender)(nil)

func (s *gateSender) Send(context.Context, *packet.Packet) error {
	select {
	case s.entered <- struct{}{}:
	default:
	}
	<-s.release
	return nil
}

func trackedPacket(payload []byte, released *atomic.Int32) *packet.Packet {
	out, rel := packet.CopyFrom(payload)
	return &packet.Packet{Envelope: packet.Envelope{Payload: out}, ReleaseFn: func() { rel(); released.Add(1) }}
}

func TestTaskSubmitBlocksWhenPoolBusyWithoutQueueSizeConfig(t *testing.T) {
	s := &gateSender{testNamedSender: testNamedSender{name: "gate"}, entered: make(chan struct{}, 2), release: make(chan struct{})}
	tk := &Task{PoolSize: 1, Senders: []sender.Sender{s}}
	if err := tk.Start(); err != nil {
		t.Fatalf("start task: %v", err)
	}
	defer tk.StopGraceful()

	ctx := context.Background()
	var r1, r2 atomic.Int32
	p1 := trackedPacket([]byte("p1"), &r1)
	p2 := trackedPacket([]byte("p2"), &r2)

	tk.Handle(ctx, p1)
	select {
	case <-s.entered:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("first packet did not enter sender")
	}

	done := make(chan struct{})
	go func() {
		tk.Handle(ctx, p2)
		close(done)
	}()
	time.Sleep(30 * time.Millisecond)
	if r2.Load() != 0 {
		t.Fatalf("second packet should not be dropped while queued, released=%d", r2.Load())
	}
	select {
	case <-done:
		t.Fatal("second submit should block until worker available")
	default:
	}

	close(s.release)
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("queued submit did not proceed after worker released")
	}
	deadline := time.Now().Add(700 * time.Millisecond)
	for (r1.Load() == 0 || r2.Load() == 0) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if r1.Load() != 1 || r2.Load() != 1 {
		t.Fatalf("both packets should complete, released1=%d released2=%d", r1.Load(), r2.Load())
	}
}

type noopSender struct {
	testNamedSender
}

var _ sender.Sender = (*noopSender)(nil)

func (s *noopSender) Send(context.Context, *packet.Packet) error { return nil }

func TestTaskStopGracefulConcurrentHandleDoesNotUseReleasedRuntime(t *testing.T) {
	for i := 0; i < 100; i++ {
		tk := &Task{
			ExecutionModel:   ExecutionModelChannel,
			ChannelQueueSize: 2,
			Senders:          []sender.Sender{&noopSender{testNamedSender: testNamedSender{name: "noop"}}},
		}
		if err := tk.Start(); err != nil {
			t.Fatalf("start task: %v", err)
		}

		var released atomic.Int32
		var wg sync.WaitGroup
		for j := 0; j < 8; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				tk.Handle(context.Background(), trackedPacket([]byte("x"), &released))
			}()
		}
		tk.StopGraceful()
		wg.Wait()
		if got := released.Load(); got != 8 {
			t.Fatalf("all packets should be released after concurrent stop, got %d", got)
		}
	}
}
