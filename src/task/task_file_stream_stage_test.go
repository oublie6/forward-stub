package task

import (
	"context"
	"sync"
	"testing"
	"time"

	"forward-stub/src/packet"
	"forward-stub/src/pipeline"
	"forward-stub/src/sender"
)

type capturePayloadSender struct {
	testNamedSender
	mu       sync.Mutex
	payloads [][]byte
}

var _ sender.Sender = (*capturePayloadSender)(nil)

func (s *capturePayloadSender) Send(context.Context, *packet.Packet) error { return nil }

func (s *capturePayloadSender) SendCapture(_ context.Context, p *packet.Packet) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := append([]byte(nil), p.Payload...)
	s.payloads = append(s.payloads, cp)
	return nil
}

func TestTaskWithSplitFileChunkStageSendsMultiplePackets(t *testing.T) {
	cap := &capturePayloadSender{testNamedSender: testNamedSender{name: "cap"}}
	// wrap sender interface
	s := senderFuncWrapper{name: "cap", fn: cap.SendCapture}
	tk := &Task{
		Name:           "split",
		ExecutionModel: ExecutionModelFastPath,
		Pipelines: []*pipeline.Pipeline{
			{Name: "p1", Stages: []pipeline.StageFunc{pipeline.SplitFileChunkToPackets(3, false)}},
		},
		Senders: []sender.Sender{s},
	}
	if err := tk.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer tk.StopGraceful()

	payload, rel := packet.CopyFrom([]byte("abcdefgh"))
	pkt := &packet.Packet{Envelope: packet.Envelope{Kind: packet.PayloadKindFileChunk, Payload: payload, Meta: packet.Meta{EOF: true}}, ReleaseFn: rel}
	tk.Handle(context.Background(), pkt)

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		cap.mu.Lock()
		n := len(cap.payloads)
		cap.mu.Unlock()
		if n == 3 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	cap.mu.Lock()
	defer cap.mu.Unlock()
	if len(cap.payloads) != 3 {
		t.Fatalf("expected 3 payloads, got %d", len(cap.payloads))
	}
	if string(cap.payloads[0]) != "abc" || string(cap.payloads[1]) != "def" || string(cap.payloads[2]) != "gh" {
		t.Fatalf("unexpected payload sequence: %+v", cap.payloads)
	}
}

type senderFuncWrapper struct {
	name string
	fn   func(context.Context, *packet.Packet) error
}

func (s senderFuncWrapper) Name() string                                     { return s.name }
func (s senderFuncWrapper) Key() string                                      { return s.name }
func (s senderFuncWrapper) Close(context.Context) error                      { return nil }
func (s senderFuncWrapper) Send(ctx context.Context, p *packet.Packet) error { return s.fn(ctx, p) }
