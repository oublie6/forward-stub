package sender

import (
	"context"
	"testing"

	"forward-stub/src/config"

	"github.com/twmb/franz-go/pkg/kgo"
)

// TestNewKafkaSenderAppliesBufferedOptions 验证 sender 包中 NewKafkaSenderAppliesBufferedOptions 的行为。
func TestNewKafkaSenderAppliesBufferedOptions(t *testing.T) {
	s, err := NewKafkaSender("k1", config.SenderConfig{
		Type:               "kafka",
		Remote:             "127.0.0.1:9092",
		Topic:              "out",
		Concurrency:        1,
		MaxBufferedBytes:   2048,
		MaxBufferedRecords: 128,
	})
	if err != nil {
		t.Fatalf("new kafka sender: %v", err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	cli := s.clients[0].Load()
	if cli == nil {
		t.Fatalf("kafka client is nil")
	}
	if got := cli.OptValue(kgo.MaxBufferedBytes).(int64); got != 2048 {
		t.Fatalf("unexpected max buffered bytes: got %d want %d", got, 2048)
	}
	if got := cli.OptValue(kgo.MaxBufferedRecords).(int64); got != 128 {
		t.Fatalf("unexpected max buffered records: got %d want %d", got, 128)
	}
}

// TestNewKafkaSenderKeepsFranzDefaultsForBufferedOptionsWhenUnset 验证 sender 包中 NewKafkaSenderKeepsFranzDefaultsForBufferedOptionsWhenUnset 的行为。
func TestNewKafkaSenderKeepsFranzDefaultsForBufferedOptionsWhenUnset(t *testing.T) {
	s, err := NewKafkaSender("k1", config.SenderConfig{
		Type:        "kafka",
		Remote:      "127.0.0.1:9092",
		Topic:       "out",
		Concurrency: 1,
	})
	if err != nil {
		t.Fatalf("new kafka sender: %v", err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	cli := s.clients[0].Load()
	if cli == nil {
		t.Fatalf("kafka client is nil")
	}
	if got := cli.OptValue(kgo.MaxBufferedBytes).(int64); got != 0 {
		t.Fatalf("unexpected default max buffered bytes: got %d want %d", got, 0)
	}
	if got := cli.OptValue(kgo.MaxBufferedRecords).(int64); got != 10000 {
		t.Fatalf("unexpected default max buffered records: got %d want %d", got, 10000)
	}
}
