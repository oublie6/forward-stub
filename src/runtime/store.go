package runtime

import (
	"context"
	"sync"
)

type Store struct {
	mu sync.Mutex

	version int64

	receivers map[string]*ReceiverState
	senders   map[string]*SenderState
	tasks     map[string]*TaskState

	pipelines map[string]*CompiledPipeline

	subs map[string]map[string]struct{}
}

func NewStore() *Store {
	return &Store{
		receivers: make(map[string]*ReceiverState),
		senders:   make(map[string]*SenderState),
		tasks:     make(map[string]*TaskState),
		pipelines: make(map[string]*CompiledPipeline),
		subs:      make(map[string]map[string]struct{}),
	}
}

func (s *Store) StopAll(ctx context.Context) error {
	s.mu.Lock()
	receivers := make([]*ReceiverState, 0, len(s.receivers))
	for _, r := range s.receivers {
		receivers = append(receivers, r)
	}
	tasks := make([]*TaskState, 0, len(s.tasks))
	for _, t := range s.tasks {
		tasks = append(tasks, t)
	}
	senders := make([]*SenderState, 0, len(s.senders))
	for _, se := range s.senders {
		senders = append(senders, se)
	}
	s.mu.Unlock()

	for _, r := range receivers {
		_ = r.Recv.Stop(ctx)
	}
	for _, t := range tasks {
		t.T.StopGraceful()
	}
	for _, se := range senders {
		_ = se.S.Close(ctx)
	}
	return nil
}
