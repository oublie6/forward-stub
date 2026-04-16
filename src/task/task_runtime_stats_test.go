package task

import (
	"context"
	"testing"

	"forward-stub/src/packet"
	"forward-stub/src/sender"
)

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

func TestRuntimeStatsAggregatesSenderAsyncStats(t *testing.T) {
	tk := &Task{
		Name:           "async",
		ExecutionModel: ExecutionModelFastPath,
		Senders: []sender.Sender{
			&testAsyncStatsSender{testNamedSender: testNamedSender{name: "s1"}, stats: sender.AsyncRuntimeStats{QueueSize: 10, QueueUsed: 4, QueueAvailable: 6, Dropped: 1, SendErrors: 2, SendSuccess: 3}},
			&testAsyncStatsSender{testNamedSender: testNamedSender{name: "s2"}, stats: sender.AsyncRuntimeStats{QueueSize: 20, QueueUsed: 5, QueueAvailable: 15, Dropped: 4, SendErrors: 5, SendSuccess: 6}},
		},
	}
	if err := tk.Start(); err != nil {
		t.Fatalf("start task: %v", err)
	}
	defer tk.StopGraceful()

	stats := tk.runtimeStats()
	if stats.Async.QueueSize != 30 || stats.Async.QueueUsed != 9 || stats.Async.QueueAvailable != 21 ||
		stats.Async.Dropped != 5 || stats.Async.SendErrors != 7 || stats.Async.SendSuccess != 9 {
		t.Fatalf("unexpected sender async stats: %+v", stats.Async)
	}
}

type testAsyncStatsSender struct {
	testNamedSender
	stats sender.AsyncRuntimeStats
}

func (s *testAsyncStatsSender) Send(context.Context, *packet.Packet) error { return nil }

func (s *testAsyncStatsSender) AsyncRuntimeStats() sender.AsyncRuntimeStats { return s.stats }
