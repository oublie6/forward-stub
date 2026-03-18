package task

import (
	"context"
	"encoding/binary"
	"sync"
	"testing"

	"forward-stub/src/packet"
	"forward-stub/src/sender"
)

// captureSender 是供 task_channel_test.go 使用的包内辅助结构。
type captureSender struct {
	mu    sync.Mutex
	items []uint32
}

// Name 提供运行时链路所需的 task 层行为。
func (s *captureSender) Name() string { return "capture" }

// Key 提供运行时链路所需的 task 层行为。
func (s *captureSender) Key() string { return "capture" }

// Close 提供运行时链路所需的 task 层行为。
func (s *captureSender) Close(context.Context) error { return nil }

// Send 提供运行时链路所需的 task 层行为。
func (s *captureSender) Send(_ context.Context, p *packet.Packet) error {
	v := binary.BigEndian.Uint32(p.Payload)
	s.mu.Lock()
	s.items = append(s.items, v)
	s.mu.Unlock()
	return nil
}

// Snapshot 提供运行时链路所需的 task 层行为。
func (s *captureSender) Snapshot() []uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]uint32, len(s.items))
	copy(out, s.items)
	return out
}

// pktNum 是供 task_channel_test.go 使用的包内辅助函数。
func pktNum(v uint32) *packet.Packet {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	payload, rel := packet.CopyFrom(b)
	return &packet.Packet{Envelope: packet.Envelope{Payload: payload}, ReleaseFn: rel}
}

// TestChannelExecutionModelKeepsOrder 验证 task 包中 ChannelExecutionModelKeepsOrder 的行为。
func TestChannelExecutionModelKeepsOrder(t *testing.T) {
	cap := &captureSender{}
	tk := &Task{Name: "ch", ExecutionModel: ExecutionModelChannel, QueueSize: 64, Senders: []sender.Sender{cap}}
	if err := tk.Start(); err != nil {
		t.Fatalf("start task: %v", err)
	}
	for i := 0; i < 100; i++ {
		tk.Handle(context.Background(), pktNum(uint32(i)))
	}
	tk.StopGraceful()

	got := cap.Snapshot()
	if len(got) != 100 {
		t.Fatalf("unexpected sent count: %d", len(got))
	}
	for i, v := range got {
		if v != uint32(i) {
			t.Fatalf("order mismatch at %d: got=%d", i, v)
		}
	}
}

// TestChannelExecutionModelDefaultsChannelQueueSizeFromQueueSize 验证 task 包中 ChannelExecutionModelDefaultsChannelQueueSizeFromQueueSize 的行为。
func TestChannelExecutionModelDefaultsChannelQueueSizeFromQueueSize(t *testing.T) {
	tk := &Task{Name: "ch-default", ExecutionModel: ExecutionModelChannel, QueueSize: 32}
	if err := tk.Start(); err != nil {
		t.Fatalf("start task: %v", err)
	}
	defer tk.StopGraceful()

	if cap(tk.ch) != 32 {
		t.Fatalf("unexpected channel capacity: got=%d want=32", cap(tk.ch))
	}
}
