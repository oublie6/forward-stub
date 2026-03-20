package task

import "testing"

func TestRuntimeStatsPoolUsesPoolFields(t *testing.T) {
	tk := &Task{Name: "pool", ExecutionModel: ExecutionModelPool, PoolSize: 3}
	if err := tk.Start(); err != nil {
		t.Fatalf("start task: %v", err)
	}
	defer tk.StopGraceful()

	stats := tk.runtimeStats()
	if stats.ExecutionModel != ExecutionModelPool {
		t.Fatalf("unexpected execution model: %+v", stats)
	}
	if stats.PoolSize != 3 {
		t.Fatalf("unexpected pool size: %+v", stats)
	}
	if stats.ChannelQueueSize != 0 || stats.ChannelQueueUsed != 0 || stats.ChannelQueueAvailable != 0 {
		t.Fatalf("pool stats should not expose channel queue fields: %+v", stats)
	}
}

func TestRuntimeStatsChannelUsesChannelFields(t *testing.T) {
	tk := &Task{Name: "channel", ExecutionModel: ExecutionModelChannel, ChannelQueueSize: 5}
	if err := tk.Start(); err != nil {
		t.Fatalf("start task: %v", err)
	}
	defer tk.StopGraceful()

	stats := tk.runtimeStats()
	if stats.ExecutionModel != ExecutionModelChannel {
		t.Fatalf("unexpected execution model: %+v", stats)
	}
	if stats.ChannelQueueSize != 5 || stats.ChannelQueueAvailable != 5 || stats.ChannelQueueUsed != 0 {
		t.Fatalf("unexpected channel stats: %+v", stats)
	}
	if stats.PoolSize != 0 || stats.WorkerPoolRunning != 0 || stats.WorkerPoolFree != 0 || stats.WorkerPoolWaiting != 0 {
		t.Fatalf("channel stats should not expose pool fields: %+v", stats)
	}
}

func TestRuntimeStatsFastPathOmitsPoolAndChannelFields(t *testing.T) {
	tk := &Task{Name: "fast", ExecutionModel: ExecutionModelFastPath}
	if err := tk.Start(); err != nil {
		t.Fatalf("start task: %v", err)
	}
	defer tk.StopGraceful()

	stats := tk.runtimeStats()
	if stats.ExecutionModel != ExecutionModelFastPath {
		t.Fatalf("unexpected execution model: %+v", stats)
	}
	if stats.PoolSize != 0 || stats.WorkerPoolRunning != 0 || stats.WorkerPoolFree != 0 || stats.WorkerPoolWaiting != 0 {
		t.Fatalf("fastpath stats should not expose pool fields: %+v", stats)
	}
	if stats.ChannelQueueSize != 0 || stats.ChannelQueueUsed != 0 || stats.ChannelQueueAvailable != 0 {
		t.Fatalf("fastpath stats should not expose channel fields: %+v", stats)
	}
}
