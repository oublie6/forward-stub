package sender

import (
	"context"
	"sync"
	"testing"
	"time"

	"forward-stub/src/config"
	"forward-stub/src/packet"
)

type skyddsWriterMock struct {
	mu      sync.Mutex
	batches [][][]byte
}

func (m *skyddsWriterMock) Write(payload []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.batches = append(m.batches, [][]byte{append([]byte(nil), payload...)})
	return nil
}
func (m *skyddsWriterMock) WriteBatch(payloads [][]byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([][]byte, len(payloads))
	for i := range payloads {
		cp[i] = append([]byte(nil), payloads[i]...)
	}
	m.batches = append(m.batches, cp)
	return nil
}
func (m *skyddsWriterMock) Close() error { return nil }

func TestSkyDDSSenderBatchFlushByNum(t *testing.T) {
	mw := &skyddsWriterMock{}
	s, err := newSkyDDSSenderWithWriter("tx", config.SenderConfig{MessageModel: "batch_octet", BatchNum: 2, BatchSize: 4096, BatchDelay: "10s"}, mw)
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}
	_ = s.Send(context.Background(), &packet.Packet{Envelope: packet.Envelope{Payload: []byte("a")}})
	_ = s.Send(context.Background(), &packet.Packet{Envelope: packet.Envelope{Payload: []byte("b")}})
	if got := len(mw.batches); got != 1 {
		t.Fatalf("expected 1 batch, got %d", got)
	}
	if len(mw.batches[0]) != 2 {
		t.Fatalf("expected 2 messages in batch")
	}
}

func TestSkyDDSSenderBatchFlushByDelay(t *testing.T) {
	mw := &skyddsWriterMock{}
	s, err := newSkyDDSSenderWithWriter("tx", config.SenderConfig{MessageModel: "batch_octet", BatchNum: 10, BatchSize: 4096, BatchDelay: "20ms"}, mw)
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}
	_ = s.Send(context.Background(), &packet.Packet{Envelope: packet.Envelope{Payload: []byte("a")}})
	time.Sleep(80 * time.Millisecond)
	if got := len(mw.batches); got != 1 {
		t.Fatalf("expected 1 delayed batch, got %d", got)
	}
}

func TestSkyDDSSenderBatchFlushOnClose(t *testing.T) {
	mw := &skyddsWriterMock{}
	s, err := newSkyDDSSenderWithWriter("tx", config.SenderConfig{MessageModel: "batch_octet", BatchNum: 10, BatchSize: 4096, BatchDelay: "10s"}, mw)
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}
	_ = s.Send(context.Background(), &packet.Packet{Envelope: packet.Envelope{Payload: []byte("a")}})
	if err := s.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
	if got := len(mw.batches); got != 1 {
		t.Fatalf("expected 1 close batch, got %d", got)
	}
}
