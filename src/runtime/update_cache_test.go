package runtime

import (
	"context"
	"strings"
	"sync"
	"testing"

	"forward-stub/src/config"
	"forward-stub/src/packet"
	"forward-stub/src/sender"
	"forward-stub/src/task"
)

type captureSender struct {
	name string

	mu      sync.Mutex
	payload [][]byte
}

var _ sender.Sender = (*captureSender)(nil)

func (s *captureSender) Name() string { return s.name }

func (s *captureSender) Key() string { return s.name }

func (s *captureSender) Send(_ context.Context, p *packet.Packet) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := append([]byte(nil), p.Payload...)
	s.payload = append(s.payload, cp)
	return nil
}

func (s *captureSender) Close(_ context.Context) error { return nil }

func (s *captureSender) Last() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.payload) == 0 {
		return nil
	}
	return s.payload[len(s.payload)-1]
}

type spyPacketSender struct {
	name string
	last *packet.Packet
}

var _ sender.Sender = (*spyPacketSender)(nil)

func (s *spyPacketSender) Name() string { return s.name }

func (s *spyPacketSender) Key() string { return s.name }

func (s *spyPacketSender) Send(_ context.Context, p *packet.Packet) error {
	s.last = p
	return nil
}

func (s *spyPacketSender) Close(_ context.Context) error { return nil }

func TestDispatchClonesForEveryTaskAndReleasesOriginal(t *testing.T) {
	ctx := context.Background()

	s1 := &captureSender{name: "s1"}
	s2 := &captureSender{name: "s2"}

	t1 := &task.Task{Name: "t1", FastPath: true, Senders: []sender.Sender{s1}}
	t2 := &task.Task{Name: "t2", FastPath: true, Senders: []sender.Sender{s2}}
	if err := t1.Start(); err != nil {
		t.Fatalf("t1 start: %v", err)
	}
	defer t1.StopGraceful()
	if err := t2.Start(); err != nil {
		t.Fatalf("t2 start: %v", err)
	}
	defer t2.StopGraceful()

	st := NewStore()
	st.setDispatchSubs(map[string][]*TaskState{
		"receiver": {
			{Name: "t1", T: t1},
			{Name: "t2", T: t2},
		},
	})

	payload := []byte("hello-kafka-and-udp")
	pkt := &packet.Packet{Envelope: packet.Envelope{Payload: append([]byte(nil), payload...)}}
	released := 0
	pkt.ReleaseFn = func() {
		released++
		// 模拟底层 buffer 被回收后，原 payload 立刻不可读。
		pkt.Payload = nil
	}

	dispatch(ctx, st, "receiver", pkt)

	if got := string(s1.Last()); got != string(payload) {
		t.Fatalf("task1 payload mismatch: got=%q want=%q", got, string(payload))
	}
	if got := string(s2.Last()); got != string(payload) {
		t.Fatalf("task2 payload mismatch: got=%q want=%q", got, string(payload))
	}
	if released != 1 {
		t.Fatalf("original packet should be released once, got=%d", released)
	}
}

func TestDispatchSingleSubscriberReusesOriginalPacket(t *testing.T) {
	ctx := context.Background()

	s1 := &spyPacketSender{name: "s1"}
	t1 := &task.Task{Name: "t1", FastPath: true, Senders: []sender.Sender{s1}}
	if err := t1.Start(); err != nil {
		t.Fatalf("t1 start: %v", err)
	}
	defer t1.StopGraceful()

	st := NewStore()
	st.setDispatchSubs(map[string][]*TaskState{
		"receiver": {
			{Name: "t1", T: t1},
		},
	})

	payload := []byte("single-subscriber")
	pkt := &packet.Packet{Envelope: packet.Envelope{Payload: append([]byte(nil), payload...)}}
	released := 0
	pkt.ReleaseFn = func() { released++ }

	dispatch(ctx, st, "receiver", pkt)

	if s1.last != pkt {
		t.Fatalf("single subscriber should receive original packet instance")
	}
	if got := string(s1.last.Payload); got != string(payload) {
		t.Fatalf("task1 payload mismatch: got=%q want=%q", got, string(payload))
	}
	if released != 1 {
		t.Fatalf("original packet should be released once by task, got=%d", released)
	}
}

type spyReceiver struct {
	name      string
	stopCalls int
}

func (r *spyReceiver) Name() string { return r.name }

func (r *spyReceiver) Key() string { return r.name }

func (r *spyReceiver) Start(_ context.Context, _ func(*packet.Packet)) error { return nil }

func (r *spyReceiver) Stop(_ context.Context) error {
	r.stopCalls++
	return nil
}

func TestReplaceAllCompileErrorKeepsExistingRuntime(t *testing.T) {
	ctx := context.Background()

	r := &spyReceiver{name: "r1"}
	s := &captureSender{name: "s1"}

	st := NewStore()
	st.receivers["r1"] = &ReceiverState{Name: "r1", Running: true, Recv: r}
	st.senders["s1"] = &SenderState{Name: "s1", S: s, Refs: 1}

	cfg := config.Config{
		Version: 2,
		Pipelines: map[string][]config.StageConfig{
			"broken": {{Type: "definitely_unknown_stage"}},
		},
	}

	err := st.replaceAll(ctx, cfg)
	if err == nil {
		t.Fatalf("replaceAll should fail for invalid pipeline")
	}
	if !strings.Contains(err.Error(), "unknown stage type") {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.stopCalls != 0 {
		t.Fatalf("existing receivers should keep running on compile failure, stop calls=%d", r.stopCalls)
	}
	if len(st.receivers) != 1 || st.receivers["r1"] == nil {
		t.Fatalf("existing runtime state should remain intact after compile failure")
	}
}
