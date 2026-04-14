package sender

import (
	"context"
	"sync"
	"testing"
	"time"

	"forward-stub/src/config"
	"forward-stub/src/packet"
	"forward-stub/src/skydds"
)

type skyddsWriterMock struct {
	mu       sync.Mutex
	writes   [][]byte
	batches  [][][]byte
	closeHit int
}

func (m *skyddsWriterMock) Write(payload []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writes = append(m.writes, append([]byte(nil), payload...))
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

func (m *skyddsWriterMock) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeHit++
	return nil
}

func (m *skyddsWriterMock) writeCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.writes)
}

func (m *skyddsWriterMock) writeAt(i int) []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]byte(nil), m.writes[i]...)
}

func (m *skyddsWriterMock) batchCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.batches)
}

func (m *skyddsWriterMock) batchAt(i int) [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([][]byte, len(m.batches[i]))
	for j := range m.batches[i] {
		cp[j] = append([]byte(nil), m.batches[i][j]...)
	}
	return cp
}

func (m *skyddsWriterMock) closeCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closeHit
}

func TestNewSkyDDSSenderPassesCommonOptions(t *testing.T) {
	orig := skyddsWriterFactory
	defer func() { skyddsWriterFactory = orig }()

	var got skydds.CommonOptions
	skyddsWriterFactory = func(opts skydds.CommonOptions) (skydds.Writer, error) {
		got = opts
		return &skyddsWriterMock{}, nil
	}

	_, err := NewSkyDDSSender("tx", config.SenderConfig{
		DCPSConfigFile:      "/tmp/dds.ini",
		DomainID:            3,
		TopicName:           "T",
		MessageModel:        "octet",
		Reliable:            true,
		QueueDepth:          128,
		MaxBlockingTimeMsec: 50,
		Compress:            true,
	})
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}
	if got.DCPSConfigFile != "/tmp/dds.ini" || got.DomainID != 3 || got.TopicName != "T" || got.MessageModel != "octet" {
		t.Fatalf("unexpected writer options: %+v", got)
	}
	if !got.Reliable || got.QueueDepth != 128 || got.MaxBlockingTimeMsec != 50 || !got.Compress {
		t.Fatalf("unexpected writer qos options: %+v", got)
	}
}

func TestSkyDDSSenderOctetWrite(t *testing.T) {
	mw := &skyddsWriterMock{}
	s, err := newSkyDDSSenderWithWriter("tx", config.SenderConfig{MessageModel: "octet"}, mw)
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}
	if err := s.Send(context.Background(), &packet.Packet{Envelope: packet.Envelope{Payload: []byte("hello")}}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if got := mw.writeCount(); got != 1 {
		t.Fatalf("expected 1 octet write, got %d", got)
	}
	if got := string(mw.writeAt(0)); got != "hello" {
		t.Fatalf("payload mismatch: %q", got)
	}
}

func TestSkyDDSSenderBatchFlushByNum(t *testing.T) {
	mw := &skyddsWriterMock{}
	s, err := newSkyDDSSenderWithWriter("tx", config.SenderConfig{MessageModel: "batch_octet", BatchNum: 2, BatchSize: 4096, BatchDelay: "10s"}, mw)
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}
	_ = s.Send(context.Background(), &packet.Packet{Envelope: packet.Envelope{Payload: []byte("a")}})
	_ = s.Send(context.Background(), &packet.Packet{Envelope: packet.Envelope{Payload: []byte("b")}})
	if got := mw.batchCount(); got != 1 {
		t.Fatalf("expected 1 batch, got %d", got)
	}
	batch := mw.batchAt(0)
	if string(batch[0]) != "a" || string(batch[1]) != "b" {
		t.Fatalf("batch order mismatch: %+v", batch)
	}
}

func TestSkyDDSSenderBatchFlushBySize(t *testing.T) {
	mw := &skyddsWriterMock{}
	s, err := newSkyDDSSenderWithWriter("tx", config.SenderConfig{MessageModel: "batch_octet", BatchNum: 10, BatchSize: 3, BatchDelay: "10s"}, mw)
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}
	_ = s.Send(context.Background(), &packet.Packet{Envelope: packet.Envelope{Payload: []byte("ab")}})
	_ = s.Send(context.Background(), &packet.Packet{Envelope: packet.Envelope{Payload: []byte("c")}})
	if got := mw.batchCount(); got != 1 {
		t.Fatalf("expected flush by size, got %d batches", got)
	}
	if got := len(mw.batchAt(0)); got != 2 {
		t.Fatalf("expected 2 items in flushed batch, got %d", got)
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
	if got := mw.batchCount(); got != 1 {
		t.Fatalf("expected 1 delayed batch, got %d", got)
	}
}

func TestSkyDDSSenderBatchOversizeSingle(t *testing.T) {
	mw := &skyddsWriterMock{}
	s, err := newSkyDDSSenderWithWriter("tx", config.SenderConfig{MessageModel: "batch_octet", BatchNum: 8, BatchSize: 4, BatchDelay: "10s"}, mw)
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}
	_ = s.Send(context.Background(), &packet.Packet{Envelope: packet.Envelope{Payload: []byte("12345")}})
	if got := mw.batchCount(); got != 1 {
		t.Fatalf("expected immediate oversize flush, got %d", got)
	}
	batch := mw.batchAt(0)
	if len(batch) != 1 || string(batch[0]) != "12345" {
		t.Fatalf("oversize payload mismatch: %+v", batch)
	}
}

func TestSkyDDSSenderBatchFlushOnClose(t *testing.T) {
	mw := &skyddsWriterMock{}
	s, err := newSkyDDSSenderWithWriter("tx", config.SenderConfig{MessageModel: "batch_octet", BatchNum: 10, BatchSize: 4096, BatchDelay: "10s"}, mw)
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}
	_ = s.Send(context.Background(), &packet.Packet{Envelope: packet.Envelope{Payload: []byte("a")}})
	_ = s.Send(context.Background(), &packet.Packet{Envelope: packet.Envelope{Payload: []byte("b")}})
	if err := s.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
	if got := mw.batchCount(); got != 1 {
		t.Fatalf("expected 1 close batch, got %d", got)
	}
	if got := mw.closeCount(); got != 1 {
		t.Fatalf("expected writer close called once, got %d", got)
	}
}
