package sender

import (
	"context"
	"errors"
	"strings"
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

type orderedNotifier struct {
	id     int
	calls  *[]int
	closed *int
	err    error
}

func (n orderedNotifier) NotifyFileReady(context.Context, FileReadyEvent) error {
	*n.calls = append(*n.calls, n.id)
	return n.err
}

func (n orderedNotifier) Close(context.Context) error {
	if n.closed != nil {
		(*n.closed)++
	}
	return nil
}

func TestNotifyFileReadyCallsAllNotifiersAndJoinsFailures(t *testing.T) {
	var calls []int
	errBoom := errors.New("boom")
	err := notifyFileReady(context.Background(), []FileReadyNotifier{
		orderedNotifier{id: 1, calls: &calls},
		orderedNotifier{id: 2, calls: &calls, err: errBoom},
		orderedNotifier{id: 3, calls: &calls},
	}, FileReadyEvent{EventType: fileReadyEventType})
	if !errors.Is(err, errBoom) {
		t.Fatalf("notify error got=%v want=%v", err, errBoom)
	}
	if !strings.Contains(err.Error(), "notifier[1]") {
		t.Fatalf("notify error should include failing notifier index, got=%v", err)
	}
	if len(calls) != 3 || calls[0] != 1 || calls[1] != 2 || calls[2] != 3 {
		t.Fatalf("notifiers should continue after failure, calls=%v", calls)
	}
}

func TestBuildFileReadyNotifiersClosesAlreadyBuiltOnLaterFailure(t *testing.T) {
	oldSkyFactory := skyddsCommitWriterFactory
	defer func() { skyddsCommitWriterFactory = oldSkyFactory }()

	closed := 0
	skyddsCommitWriterFactory = func(skydds.CommonOptions) (skydds.Writer, error) {
		return closeCountingSkyDDSWriter{closed: &closed}, nil
	}

	_, err := buildFileReadyNotifiers(config.NotifyOnSuccessConfigs{
		{Type: "dds_skydds", DCPSConfigFile: "dds.ini", DomainID: 0, TopicName: "ready", MessageModel: "octet"},
		{Type: "unknown"},
	})
	if err == nil {
		t.Fatalf("expected build failure")
	}
	if closed != 1 {
		t.Fatalf("already built notifier should be closed once, got %d", closed)
	}
}

type closeCountingSkyDDSWriter struct {
	closed *int
}

func (w closeCountingSkyDDSWriter) Write([]byte) error        { return nil }
func (w closeCountingSkyDDSWriter) WriteBatch([][]byte) error { return nil }
func (w closeCountingSkyDDSWriter) Close() error {
	*w.closed++
	return nil
}
