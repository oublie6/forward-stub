package task

import (
	"context"
	"sync"
	"testing"
	"time"

	"forward-stub/src/packet"
	"forward-stub/src/sender"
)

// namedCaptureSender stores package-local state used by task_route_sender_test.go.
type namedCaptureSender struct {
	name  string
	mu    sync.Mutex
	count int
}

var _ sender.Sender = (*namedCaptureSender)(nil)

// Name provides task-level behavior used by the runtime pipeline.
func (s *namedCaptureSender) Name() string { return s.name }

// Key provides task-level behavior used by the runtime pipeline.
func (s *namedCaptureSender) Key() string { return s.name }

// Close provides task-level behavior used by the runtime pipeline.
func (s *namedCaptureSender) Close(context.Context) error { return nil }

// Send provides task-level behavior used by the runtime pipeline.
func (s *namedCaptureSender) Send(context.Context, *packet.Packet) error {
	s.mu.Lock()
	s.count++
	s.mu.Unlock()
	return nil
}

// Count provides task-level behavior used by the runtime pipeline.
func (s *namedCaptureSender) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

// TestTaskRouteSenderSendsOnlyMatchedSender verifies the TaskRouteSenderSendsOnlyMatchedSender behavior for the task package.
func TestTaskRouteSenderSendsOnlyMatchedSender(t *testing.T) {
	s1 := &namedCaptureSender{name: "kafka-a"}
	s2 := &namedCaptureSender{name: "kafka-b"}
	tk := &Task{Name: "route", ExecutionModel: ExecutionModelFastPath, Senders: []sender.Sender{s1, s2}}
	if err := tk.Start(); err != nil {
		t.Fatalf("start task: %v", err)
	}
	defer tk.StopGraceful()

	payload, rel := packet.CopyFrom([]byte("x"))
	pkt := &packet.Packet{Envelope: packet.Envelope{Payload: payload, Meta: packet.Meta{RouteSender: "kafka-b"}}, ReleaseFn: rel}
	tk.Handle(context.Background(), pkt)

	deadline := time.Now().Add(200 * time.Millisecond)
	for s1.Count()+s2.Count() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if s1.Count() != 0 || s2.Count() != 1 {
		t.Fatalf("unexpected send counts: s1=%d s2=%d", s1.Count(), s2.Count())
	}
}

// BenchmarkTaskRouteSenderLookup benchmarks the TaskRouteSenderLookup behavior for the task package.
func BenchmarkTaskRouteSenderLookup(b *testing.B) {
	senders := make([]sender.Sender, 0, 64)
	for i := 0; i < 64; i++ {
		senders = append(senders, &namedCaptureSender{name: "s" + string(rune('A'+(i%26))) + string(rune('a'+(i/26)))})
	}
	// ensure a deterministic target exists.
	target := &namedCaptureSender{name: "target"}
	senders = append(senders, target)
	tk := &Task{Name: "bench-route", ExecutionModel: ExecutionModelFastPath, Senders: senders}
	if err := tk.Start(); err != nil {
		b.Fatalf("start task: %v", err)
	}
	defer tk.StopGraceful()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		payload, rel := packet.CopyFrom([]byte("x"))
		pkt := &packet.Packet{Envelope: packet.Envelope{Payload: payload, Meta: packet.Meta{RouteSender: "target"}}, ReleaseFn: rel}
		tk.Handle(context.Background(), pkt)
	}
}
