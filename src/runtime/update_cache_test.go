// Package runtime 负责维护转发运行时对象及其测试辅助逻辑。
package runtime

import (
	"context"
	"sync"
	"testing"

	"forward-stub/src/packet"
	"forward-stub/src/sender"
	"forward-stub/src/task"
)

// captureSender 是测试用发送端，会把收到的 payload 副本保存在内存中供断言使用。
type captureSender struct {
	testNamedSender

	// mu 保护 payload 切片，避免测试中并发发送造成数据竞争。
	mu sync.Mutex
	// payload 保存每次发送的内容副本，便于回放最后一次或全部发送结果。
	payload [][]byte
}

// 确保 captureSender 始终满足 sender.Sender 接口。
var _ sender.Sender = (*captureSender)(nil)

// Send 复制并保存 payload，避免后续释放底层 buffer 影响断言。
func (s *captureSender) Send(_ context.Context, p *packet.Packet) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := append([]byte(nil), p.Payload...)
	s.payload = append(s.payload, cp)
	return nil
}

// Last 返回最近一次发送的 payload 副本；若尚未发送则返回 nil。
func (s *captureSender) Last() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.payload) == 0 {
		return nil
	}
	return s.payload[len(s.payload)-1]
}

// spyPacketSender 是测试用发送端，用于观察任务收到的是否为同一个 packet 指针。
type spyPacketSender struct {
	testNamedSender
	// last 保存最近一次收到的 packet 指针，便于测试是否发生 clone。
	last *packet.Packet
}

// 确保 spyPacketSender 始终满足 sender.Sender 接口。
var _ sender.Sender = (*spyPacketSender)(nil)

// Send 仅记录 packet 指针，不复制内容，便于验证复用策略。
func (s *spyPacketSender) Send(_ context.Context, p *packet.Packet) error {
	s.last = p
	return nil
}

// TestDispatchClonesForEveryTaskAndReleasesOriginal 验证多订阅场景会为后续任务 clone 包并释放原包一次。
func TestDispatchClonesForEveryTaskAndReleasesOriginal(t *testing.T) {
	ctx := context.Background()

	s1 := &captureSender{testNamedSender: testNamedSender{name: "s1"}}
	s2 := &captureSender{testNamedSender: testNamedSender{name: "s2"}}

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

// TestDispatchSingleSubscriberReusesOriginalPacket 验证单订阅场景直接复用原始 packet，避免多余复制。
func TestDispatchSingleSubscriberReusesOriginalPacket(t *testing.T) {
	ctx := context.Background()

	s1 := &spyPacketSender{testNamedSender: testNamedSender{name: "s1"}}
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
