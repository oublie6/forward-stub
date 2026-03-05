package task

import (
	"context"
	"encoding/binary"
	"sync"
	"testing"

	"forward-stub/src/packet"
	"forward-stub/src/sender"
)

type captureSender struct {
	mu    sync.Mutex
	items []uint32
}

func (s *captureSender) Name() string                { return "capture" }
func (s *captureSender) Key() string                 { return "capture" }
func (s *captureSender) Close(context.Context) error { return nil }
func (s *captureSender) Send(_ context.Context, p *packet.Packet) error {
	v := binary.BigEndian.Uint32(p.Payload)
	s.mu.Lock()
	s.items = append(s.items, v)
	s.mu.Unlock()
	return nil
}

func (s *captureSender) Snapshot() []uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]uint32, len(s.items))
	copy(out, s.items)
	return out
}

func pktNum(v uint32) *packet.Packet {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	payload, rel := packet.CopyFrom(b)
	return &packet.Packet{Envelope: packet.Envelope{Payload: payload}, ReleaseFn: rel}
}

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
