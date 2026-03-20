package logx

import (
	"testing"
	"time"
)

// TestTrafficSummaryTaskAggregateIncludesWorkerPoolStats 验证 task 聚合日志不仅统计流量，
// 还会把运行时 worker pool / queue / inflight 状态一起写入摘要。
//
// 该用例覆盖“有流量 + 有 runtime stats”的主路径，是后续维护聚合日志字段时的回归基线。
func TestTrafficSummaryTaskAggregateIncludesWorkerPoolStats(t *testing.T) {
	// 注册一个 task 运行时回调，模拟 task.Start 后聚合线程读取执行模型快照。
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

	// 构造一次 flush 摘要，并人为注入 task counter 的累计值。
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

// TestTrafficSummaryIncludesRuntimeOnlyTaskWithoutTraffic 验证“当前周期零流量”的 task
// 仍会因为 runtimeStats 注册而进入摘要，避免运维误判 task 已丢失或未启动。
func TestTrafficSummaryIncludesRuntimeOnlyTaskWithoutTraffic(t *testing.T) {
	// 仅提供运行时状态，不注入任何 packets/bytes。
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
