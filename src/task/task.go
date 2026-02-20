package task

import (
	"context"
	"sync"
	"sync/atomic"

	"forword-stub/src/packet"
	"forword-stub/src/pipeline"
	"forword-stub/src/sender"

	"github.com/panjf2000/ants/v2"
)

type Task struct {
	Name string

	Pipelines []*pipeline.Pipeline
	Senders   []sender.Sender

	PoolSize int
	FastPath bool

	pool *ants.Pool

	accepting atomic.Bool
	inflight  sync.WaitGroup
}

func (t *Task) Start() error {
	if t.PoolSize <= 0 {
		t.PoolSize = 64
	}
	p, err := ants.NewPool(t.PoolSize, ants.WithNonblocking(true))
	if err != nil {
		return err
	}
	t.pool = p
	t.accepting.Store(true)
	return nil
}

func (t *Task) Handle(ctx context.Context, pkt *packet.Packet) {
	if !t.accepting.Load() {
		pkt.Release()
		return
	}

	t.inflight.Add(1)
	run := func() {
		defer t.inflight.Done()
		defer pkt.Release()
		t.processAndSend(ctx, pkt)
	}

	if t.FastPath {
		run()
		return
	}

	if err := t.pool.Submit(run); err != nil {
		t.inflight.Done()
		pkt.Release()
		return
	}
}

func (t *Task) StopGraceful() {
	t.accepting.Store(false)
	t.inflight.Wait()
	if t.pool != nil {
		t.pool.Release()
		t.pool = nil
	}
}

func (t *Task) processAndSend(ctx context.Context, pkt *packet.Packet) {
	for _, pl := range t.Pipelines {
		if !pl.Process(pkt) {
			return
		}
	}
	for _, s := range t.Senders {
		_ = s.Send(ctx, pkt)
	}
}
