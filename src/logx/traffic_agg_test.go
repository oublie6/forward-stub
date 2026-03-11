package logx

import (
	"testing"
	"time"
)

func TestTrafficSummaryTaskAggregateIncludesWorkerPoolStats(t *testing.T) {
	RegisterTaskRuntimeStats("task-a", func() TaskRuntimeStats {
		return TaskRuntimeStats{
			PoolSize:       64,
			PoolRunning:    8,
			PoolFree:       56,
			PoolWaiting:    2,
			QueueSize:      128,
			QueueAvailable: 126,
			Inflight:       3,
			FastPath:       false,
		}
	})
	defer UnregisterTaskRuntimeStats("task-a")

	s := newTrafficSummary(3 * time.Second)
	c := &trafficCounter{role: "task", name: "task-a", key: "send"}
	s.add(c, 10, 100, time.Second)
	if len(s.Tasks) != 1 {
		t.Fatalf("expected one task summary item, got %d", len(s.Tasks))
	}
	item := s.Tasks[0]
	if item.Task != "task-a" {
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
		item.WorkerPool.Waiting != 2 || item.WorkerPool.QueueSize != 128 || item.WorkerPool.QueueAvailable != 126 ||
		item.WorkerPool.Inflight != 3 || item.WorkerPool.FastPath {
		t.Fatalf("unexpected worker pool stats: %+v", item.WorkerPool)
	}
}

func TestTrafficSummaryIncludesRuntimeOnlyTaskWithoutTraffic(t *testing.T) {
	stats := TaskRuntimeStats{PoolSize: 16, QueueSize: 64}
	s := newTrafficSummary(time.Second)
	s.addRuntimeOnlyTask("task-empty", stats)
	if len(s.Tasks) != 1 {
		t.Fatalf("expected one runtime-only task, got %d", len(s.Tasks))
	}
	item := s.Tasks[0]
	if item.Task != "task-empty" {
		t.Fatalf("unexpected runtime-only task item: %+v", item)
	}
	if item.TotalPackets != 0 || item.TotalBytes != 0 {
		t.Fatalf("runtime-only task should have zero counters: %+v", item)
	}
	if item.WorkerPool == nil || item.WorkerPool.Size != 16 || item.WorkerPool.QueueSize != 64 {
		t.Fatalf("unexpected runtime-only worker stats: %+v", item.WorkerPool)
	}
}
