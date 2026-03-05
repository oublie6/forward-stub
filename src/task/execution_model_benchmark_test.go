package task

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"forward-stub/src/packet"
	"forward-stub/src/sender"
)

type noopSender struct{}

func (noopSender) Name() string                                   { return "noop" }
func (noopSender) Key() string                                    { return "noop" }
func (noopSender) Send(_ context.Context, _ *packet.Packet) error { return nil }
func (noopSender) Close(_ context.Context) error                  { return nil }

func makePkt(payload []byte) *packet.Packet {
	buf, rel := packet.CopyFrom(payload)
	return &packet.Packet{Envelope: packet.Envelope{Payload: buf}, ReleaseFn: rel}
}

type chanTask struct {
	ch        chan *packet.Packet
	wg        sync.WaitGroup
	accepting atomic.Bool
}

func newChanTask(queue int) *chanTask {
	if queue <= 0 {
		queue = 4096
	}
	ct := &chanTask{ch: make(chan *packet.Packet, queue)}
	ct.accepting.Store(true)
	go func() {
		for pkt := range ct.ch {
			pkt.Release()
			ct.wg.Done()
		}
	}()
	return ct
}

func (ct *chanTask) Handle(pkt *packet.Packet) {
	if !ct.accepting.Load() {
		pkt.Release()
		return
	}
	ct.wg.Add(1)
	ct.ch <- pkt
}

func (ct *chanTask) Stop() {
	ct.accepting.Store(false)
	ct.wg.Wait()
	close(ct.ch)
}

func BenchmarkTaskExecutionModels(b *testing.B) {
	payload := make([]byte, 256)
	ctx := context.Background()
	senders := []sender.Sender{noopSender{}}

	b.Run("fastpath_sync", func(b *testing.B) {
		t := &Task{Name: "fast", FastPath: true, PoolSize: 1, QueueSize: 4096, Senders: senders}
		if err := t.Start(); err != nil {
			b.Fatalf("start task: %v", err)
		}
		defer t.StopGraceful()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			t.Handle(ctx, makePkt(payload))
		}
	})

	b.Run("task_pool_size_1", func(b *testing.B) {
		t := &Task{Name: "pool1", FastPath: false, PoolSize: 1, QueueSize: 4096, Senders: senders}
		if err := t.Start(); err != nil {
			b.Fatalf("start task: %v", err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			t.Handle(ctx, makePkt(payload))
		}
		b.StopTimer()
		t.StopGraceful()
	})

	b.Run("single_goroutine_channel", func(b *testing.B) {
		ct := newChanTask(4096)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ct.Handle(makePkt(payload))
		}
		b.StopTimer()
		ct.Stop()
	})
}
