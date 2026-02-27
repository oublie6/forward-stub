package logx

import (
	"strings"
	"testing"
	"time"

	"forward-stub/src/packet"
)

func TestTrafficSummaryTaskAggregateIncludesWorkerPoolStats(t *testing.T) {
	RegisterTaskRuntimeStats("task-a", func() TaskRuntimeStats {
		return TaskRuntimeStats{
			PoolSize:    64,
			PoolRunning: 8,
			PoolFree:    56,
			PoolWaiting: 2,
			Inflight:    3,
			FastPath:    false,
		}
	})
	defer UnregisterTaskRuntimeStats("task-a")

	s := newTrafficSummary(3 * time.Second)
	c := &trafficCounter{role: "task", name: "task-a", key: "send"}
	s.add(c, 10, 100, time.Second)
	if len(s.tasks) != 1 {
		t.Fatalf("expected one task summary item, got %d", len(s.tasks))
	}
	item := s.tasks[0]
	if item.Task != "task-a" || item.Direction != "send" {
		t.Fatalf("unexpected task aggregate identity: %+v", item)
	}
	if item.TotalPackets != 10 || item.TotalBytes != 100 {
		t.Fatalf("unexpected total counters: %+v", item)
	}
	if item.IntervalPackets != 10 || item.IntervalBytes != 100 {
		t.Fatalf("unexpected interval counters: %+v", item)
	}
	if item.PPS != 10 || item.BPS != 100 {
		t.Fatalf("unexpected rates: %+v", item)
	}
	if item.WorkerPool == nil {
		t.Fatalf("worker pool stats missing: %+v", item)
	}
	if item.WorkerPool.Size != 64 || item.WorkerPool.Running != 8 || item.WorkerPool.Free != 56 ||
		item.WorkerPool.Waiting != 2 || item.WorkerPool.Inflight != 3 || item.WorkerPool.FastPath {
		t.Fatalf("unexpected worker pool stats: %+v", item.WorkerPool)
	}
}

func TestTrafficSummaryMemoryPoolRenderedOnceInSummary(t *testing.T) {
	payload, rel := packet.CopyFrom([]byte("hello"))
	if len(payload) == 0 {
		t.Fatalf("expected non-empty payload")
	}
	defer rel()

	s := newTrafficSummary(2 * time.Second)
	s.setMemoryPool()
	checks := []string{
		"inuse_buffers=",
		"inuse_bytes=",
		"cached_bytes=",
		"total_bytes=",
		"gets=",
		"puts=",
	}
	for _, want := range checks {
		if !strings.Contains(s.memoryPool, want) {
			t.Fatalf("memory pool summary missing %q: %s", want, s.memoryPool)
		}
	}
}
