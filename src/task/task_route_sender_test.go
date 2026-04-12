package task

import (
	"context"
	"sync"
	"testing"
	"time"

	"forward-stub/src/packet"
	"forward-stub/src/sender"
)

type namedCaptureSender struct {
	testNamedSender
	mu    sync.Mutex
	count int
}

var _ sender.Sender = (*namedCaptureSender)(nil)

func (s *namedCaptureSender) Send(context.Context, *packet.Packet) error {
	s.mu.Lock()
	s.count++
	s.mu.Unlock()
	return nil
}

func (s *namedCaptureSender) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

func TestTaskRouteSenderSendsOnlyMatchedSender(t *testing.T) {
	s1 := &namedCaptureSender{testNamedSender: testNamedSender{name: "kafka-a"}}
	s2 := &namedCaptureSender{testNamedSender: testNamedSender{name: "kafka-b"}}
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

func TestTaskRouteSenderMissDropsWithoutDefaultFanout(t *testing.T) {
	s1 := &namedCaptureSender{testNamedSender: testNamedSender{name: "kafka-a"}}
	s2 := &namedCaptureSender{testNamedSender: testNamedSender{name: "kafka-b"}}
	tk := &Task{Name: "route-miss", ExecutionModel: ExecutionModelFastPath, Senders: []sender.Sender{s1, s2}}
	if err := tk.Start(); err != nil {
		t.Fatalf("start task: %v", err)
	}
	defer tk.StopGraceful()

	payload, rel := packet.CopyFrom([]byte("x"))
	pkt := &packet.Packet{Envelope: packet.Envelope{Payload: payload, Meta: packet.Meta{RouteSender: "missing"}}, ReleaseFn: rel}
	tk.Handle(context.Background(), pkt)

	if s1.Count() != 0 || s2.Count() != 0 {
		t.Fatalf("route miss should not fan out to default senders: s1=%d s2=%d", s1.Count(), s2.Count())
	}
}

func TestTaskNoRouteSenderFansOutToAllSenders(t *testing.T) {
	s1 := &namedCaptureSender{testNamedSender: testNamedSender{name: "kafka-a"}}
	s2 := &namedCaptureSender{testNamedSender: testNamedSender{name: "kafka-b"}}
	tk := &Task{Name: "fanout", ExecutionModel: ExecutionModelFastPath, Senders: []sender.Sender{s1, s2}}
	if err := tk.Start(); err != nil {
		t.Fatalf("start task: %v", err)
	}
	defer tk.StopGraceful()

	payload, rel := packet.CopyFrom([]byte("x"))
	pkt := &packet.Packet{Envelope: packet.Envelope{Payload: payload}, ReleaseFn: rel}
	tk.Handle(context.Background(), pkt)

	if s1.Count() != 1 || s2.Count() != 1 {
		t.Fatalf("no route should fan out to all senders: s1=%d s2=%d", s1.Count(), s2.Count())
	}
}

func BenchmarkTaskRouteSenderLookup(b *testing.B) {
	senders := make([]sender.Sender, 0, 64)
	for i := 0; i < 64; i++ {
		senders = append(senders, &namedCaptureSender{testNamedSender: testNamedSender{name: "s" + string(rune('A'+(i%26))) + string(rune('a'+(i/26)))}})
	}
	// ensure a deterministic target exists.
	target := &namedCaptureSender{testNamedSender: testNamedSender{name: "target"}}
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

func BenchmarkTaskRouteSenderLookupStaticPacket(b *testing.B) {
	senders := make([]sender.Sender, 0, 64)
	for i := 0; i < 64; i++ {
		senders = append(senders, &namedCaptureSender{testNamedSender: testNamedSender{name: "s" + string(rune('A'+(i%26))) + string(rune('a'+(i/26)))}})
	}
	target := &namedCaptureSender{testNamedSender: testNamedSender{name: "target"}}
	senders = append(senders, target)
	tk := &Task{Name: "bench-route-static", ExecutionModel: ExecutionModelFastPath, Senders: senders}
	if err := tk.Start(); err != nil {
		b.Fatalf("start task: %v", err)
	}
	defer tk.StopGraceful()

	pkt := &packet.Packet{Envelope: packet.Envelope{Payload: []byte("x"), Meta: packet.Meta{RouteSender: "target"}}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		tk.Handle(context.Background(), pkt)
	}
}
