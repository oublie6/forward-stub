package logx

import (
	"encoding/json"
	"testing"
	"time"
)

// TestTrafficSummaryTaskAggregateIncludesWorkerPoolStats 验证 pool 模式 task 聚合日志不仅统计流量，
// 还会把运行时 worker pool / inflight 状态一起写入摘要。
//
// 该用例覆盖“有流量 + 有 runtime stats”的主路径，是后续维护聚合日志字段时的回归基线。
func TestTrafficSummaryTaskAggregateIncludesWorkerPoolStats(t *testing.T) {
	// 注册一个 task 运行时回调，模拟 task.Start 后聚合线程读取执行模型快照。
	RegisterTaskRuntimeStats("task-a", func() TaskRuntimeStats {
		return TaskRuntimeStats{
			ExecutionModel:    "pool",
			PoolSize:          64,
			WorkerPoolRunning: 8,
			WorkerPoolFree:    56,
			WorkerPoolWaiting: 2,
			Inflight:          3,
			RouteSenderMiss:   4,
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
	if item.ExecutionModel != "pool" || item.Inflight != 3 || item.PoolSize != 64 {
		t.Fatalf("unexpected runtime summary: %+v", item)
	}
	if item.RouteSenderMiss != 4 {
		t.Fatalf("unexpected route sender miss counter: %+v", item)
	}
	if item.WorkerPool == nil {
		t.Fatalf("worker pool stats missing: %+v", item)
	}
	if item.WorkerPool.Running != 8 || item.WorkerPool.Free != 56 || item.WorkerPool.Waiting != 2 {
		t.Fatalf("unexpected worker pool stats: %+v", item.WorkerPool)
	}
	if item.Channel != nil {
		t.Fatalf("pool task should not expose channel stats: %+v", item.Channel)
	}
}

func TestTrafficSummaryIncludesChannelRuntimeOnlyTaskWithoutTraffic(t *testing.T) {
	stats := TaskRuntimeStats{
		ExecutionModel:        "channel",
		ChannelQueueSize:      64,
		ChannelQueueUsed:      2,
		ChannelQueueAvailable: 62,
		Inflight:              1,
	}
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
	if item.ExecutionModel != "channel" || item.Inflight != 1 {
		t.Fatalf("unexpected runtime-only channel summary: %+v", item)
	}
	if item.WorkerPool != nil {
		t.Fatalf("channel task should not expose worker_pool stats: %+v", item.WorkerPool)
	}
	if item.Channel == nil || item.Channel.QueueSize != 64 || item.Channel.QueueUsed != 2 || item.Channel.QueueAvailable != 62 {
		t.Fatalf("unexpected runtime-only channel stats: %+v", item.Channel)
	}
}

func TestTrafficSummaryFastPathOmitsPoolAndChannelStats(t *testing.T) {
	stats := TaskRuntimeStats{ExecutionModel: "fastpath", Inflight: 4}
	s := newTrafficSummary(time.Second)
	s.addRuntimeOnlyTask("task-fastpath", stats)
	if len(s.Tasks) != 1 {
		t.Fatalf("expected one runtime-only task, got %d", len(s.Tasks))
	}
	item := s.Tasks[0]
	if item.ExecutionModel != "fastpath" || item.Inflight != 4 {
		t.Fatalf("unexpected fastpath runtime summary: %+v", item)
	}
	if item.WorkerPool != nil || item.Channel != nil || item.PoolSize != 0 {
		t.Fatalf("fastpath should not expose pool/channel stats: %+v", item)
	}
}

func TestTrafficSummaryIncludesSenderRuntimeStats(t *testing.T) {
	s := newTrafficSummary(time.Second)
	s.addSenderRuntime("kafka-a", SenderRuntimeStats{
		QueueSize:      16,
		QueueUsed:      5,
		QueueAvailable: 11,
		Dropped:        2,
		SendErrors:     3,
		SendSuccess:    7,
	})
	if len(s.Senders) != 1 {
		t.Fatalf("expected one sender item, got %d", len(s.Senders))
	}
	item := s.Senders[0]
	if item.Sender != "kafka-a" {
		t.Fatalf("unexpected sender identity: %+v", item)
	}
	if item.QueueSize != 16 || item.QueueUsed != 5 || item.QueueAvailable != 11 ||
		item.Dropped != 2 || item.SendErrors != 3 || item.SendSuccess != 7 {
		t.Fatalf("unexpected sender stats: %+v", item)
	}
}

func TestSenderRuntimeStatsRegisteredOnceBySenderName(t *testing.T) {
	unregisterOld := RegisterSenderRuntimeStats("kafka-shared", func() SenderRuntimeStats {
		return SenderRuntimeStats{QueueSize: 8, QueueUsed: 3, QueueAvailable: 5}
	})
	unregisterNew := RegisterSenderRuntimeStats("kafka-shared", func() SenderRuntimeStats {
		return SenderRuntimeStats{QueueSize: 8, QueueUsed: 4, QueueAvailable: 4}
	})
	defer unregisterNew()

	unregisterOld()

	stats := listSenderRuntimeStats()
	if len(stats) != 1 {
		t.Fatalf("expected one sender runtime entry, got %d", len(stats))
	}
	got := stats["kafka-shared"]
	if got.QueueUsed != 4 || got.QueueAvailable != 4 {
		t.Fatalf("expected latest sender runtime callback, got %+v", got)
	}
}

func TestTrafficSummarySharedSenderNotRepeatedPerTask(t *testing.T) {
	s := newTrafficSummary(time.Second)
	s.addRuntimeOnlyTask("task-a", TaskRuntimeStats{ExecutionModel: "fastpath"})
	s.addRuntimeOnlyTask("task-b", TaskRuntimeStats{ExecutionModel: "fastpath"})
	s.addSenderRuntime("kafka-shared", SenderRuntimeStats{QueueSize: 8, QueueUsed: 3, QueueAvailable: 5})

	if len(s.Tasks) != 2 {
		t.Fatalf("expected two task entries, got %d", len(s.Tasks))
	}
	if len(s.Senders) != 1 {
		t.Fatalf("shared sender should appear once, got %d entries", len(s.Senders))
	}
	if s.Senders[0].Sender != "kafka-shared" {
		t.Fatalf("unexpected sender entry: %+v", s.Senders[0])
	}
}

func TestTrafficSummaryTaskDoesNotCarrySenderAsyncStats(t *testing.T) {
	s := newTrafficSummary(time.Second)
	s.addRuntimeOnlyTask("task-a", TaskRuntimeStats{ExecutionModel: "pool", Inflight: 1})
	s.addSenderRuntime("kafka-a", SenderRuntimeStats{QueueSize: 16, QueueUsed: 5, QueueAvailable: 11})

	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	var raw struct {
		Tasks []map[string]any `json:"tasks"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal summary: %v", err)
	}
	if len(raw.Tasks) != 1 {
		t.Fatalf("expected one task, got %d", len(raw.Tasks))
	}
	if _, ok := raw.Tasks[0]["async"]; ok {
		t.Fatalf("task should not carry sender async stats: %s", b)
	}
}
