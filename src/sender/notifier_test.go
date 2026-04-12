package sender

import (
	"context"
	"testing"

	"forward-stub/src/config"
	"forward-stub/src/skydds"
)

type fakeSkyDDSWriter struct {
	writes int
}

func (w *fakeSkyDDSWriter) Write([]byte) error {
	w.writes++
	return nil
}
func (w *fakeSkyDDSWriter) WriteBatch([][]byte) error { return nil }
func (w *fakeSkyDDSWriter) Close() error              { return nil }

func TestSkyDDSCommitNotifierRejectsNonOctet(t *testing.T) {
	if _, err := NewSkyDDSCommitNotifier(config.NotifyOnSuccessConfig{MessageModel: "batch_octet"}); err == nil {
		t.Fatalf("expected non-octet error")
	}
}

func TestSkyDDSCommitNotifierWritesEvent(t *testing.T) {
	old := skyddsCommitWriterFactory
	w := &fakeSkyDDSWriter{}
	skyddsCommitWriterFactory = func(skydds.CommonOptions) (skydds.Writer, error) { return w, nil }
	defer func() { skyddsCommitWriterFactory = old }()

	n, err := NewSkyDDSCommitNotifier(config.NotifyOnSuccessConfig{
		DCPSConfigFile: "dds.ini",
		DomainID:       1,
		TopicName:      "ready",
		MessageModel:   "octet",
	})
	if err != nil {
		t.Fatalf("new notifier: %v", err)
	}
	if err := n.NotifyFileReady(context.Background(), FileReadyEvent{EventType: fileReadyEventType}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if w.writes != 1 {
		t.Fatalf("writes got=%d", w.writes)
	}
}

func TestKafkaCommitNotifierConstructsWithoutDial(t *testing.T) {
	n, err := NewKafkaCommitNotifier(config.NotifyOnSuccessConfig{
		Remote:          "127.0.0.1:9092",
		Topic:           "ready",
		RecordKeySource: "transfer_id",
	})
	if err != nil {
		t.Fatalf("new kafka notifier: %v", err)
	}
	_ = n.Close(context.Background())
	if got := fileReadyRecordKey("transfer_id", FileReadyEvent{TransferID: "tx"}); string(got) != "tx" {
		t.Fatalf("record key got=%q", got)
	}
}
