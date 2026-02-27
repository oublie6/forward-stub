package logx

import (
	"strings"
	"testing"
	"time"

	"forward-stub/src/packet"
)

func TestTrafficSummaryTaskLineIncludesWorkerPoolStats(t *testing.T) {
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
	if len(s.task) != 1 {
		t.Fatalf("expected one task summary line, got %d", len(s.task))
	}
	line := s.task[0]
	checks := []string{
		"worker_pool={size=64 running=8 free=56 waiting=2 inflight=3 fast_path=false}",
		"interval_packets=10",
		"interval_bytes=100",
	}
	for _, want := range checks {
		if !strings.Contains(line, want) {
			t.Fatalf("summary line missing %q: %s", want, line)
		}
	}
	if strings.Contains(line, "memory_pool={") {
		t.Fatalf("task line should not contain memory_pool anymore: %s", line)
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
