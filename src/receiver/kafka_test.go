package receiver

import (
	"context"
	"testing"
	"time"

	"forward-stub/src/config"

	"github.com/twmb/franz-go/pkg/kgo"
)

func TestNewKafkaReceiverAppliesKafkaGroupAndFetchOptions(t *testing.T) {
	autoCommit := true
	r, err := NewKafkaReceiver("r1", config.ReceiverConfig{
		Type:                   "kafka",
		Listen:                 "127.0.0.1:9092",
		Topic:                  "in",
		GroupID:                "g1",
		DialTimeout:            "11s",
		ConnIdleTimeout:        "44s",
		MetadataMaxAge:         "6m",
		RetryBackoff:           "350ms",
		SessionTimeout:         "50s",
		HeartbeatInterval:      "4s",
		RebalanceTimeout:       "70s",
		Balancers:              []string{"range", "cooperative_sticky"},
		AutoCommit:             &autoCommit,
		AutoCommitInterval:     "7s",
		FetchMaxPartitionBytes: 2 << 20,
		IsolationLevel:         "read_committed",
	})
	if err != nil {
		t.Fatalf("new kafka receiver: %v", err)
	}
	t.Cleanup(func() { _ = r.Stop(context.Background()) })

	if got := r.client.OptValue(kgo.DialTimeout).(time.Duration); got != 11*time.Second {
		t.Fatalf("unexpected dial timeout: %s", got)
	}
	if got := r.client.OptValue(kgo.ConnIdleTimeout).(time.Duration); got != 44*time.Second {
		t.Fatalf("unexpected conn idle timeout: %s", got)
	}
	if got := r.client.OptValue(kgo.MetadataMaxAge).(time.Duration); got != 6*time.Minute {
		t.Fatalf("unexpected metadata max age: %s", got)
	}
	if got := r.client.OptValue(kgo.SessionTimeout).(time.Duration); got != 50*time.Second {
		t.Fatalf("unexpected session timeout: %s", got)
	}
	if got := r.client.OptValue(kgo.HeartbeatInterval).(time.Duration); got != 4*time.Second {
		t.Fatalf("unexpected heartbeat interval: %s", got)
	}
	if got := r.client.OptValue(kgo.RebalanceTimeout).(time.Duration); got != 70*time.Second {
		t.Fatalf("unexpected rebalance timeout: %s", got)
	}
	if got := r.client.OptValue(kgo.FetchMaxPartitionBytes).(int32); got != 2<<20 {
		t.Fatalf("unexpected fetch max partition bytes: %d", got)
	}
	if got := r.client.OptValue(kgo.AutoCommitInterval).(time.Duration); got != 7*time.Second {
		t.Fatalf("unexpected auto commit interval: %s", got)
	}
	if got := r.client.OptValue(kgo.FetchIsolationLevel).(int8); got != 1 {
		t.Fatalf("unexpected isolation level: %d", got)
	}
	balancers := r.client.OptValue(kgo.Balancers).([]kgo.GroupBalancer)
	if len(balancers) != 2 || balancers[0].ProtocolName() != kgo.RangeBalancer().ProtocolName() || balancers[1].ProtocolName() != kgo.CooperativeStickyBalancer().ProtocolName() {
		t.Fatalf("unexpected balancers: %#v", balancers)
	}
}

func TestNewKafkaReceiverDisablesAutoCommitWhenConfigured(t *testing.T) {
	autoCommit := false
	r, err := NewKafkaReceiver("r1", config.ReceiverConfig{
		Type:       "kafka",
		Listen:     "127.0.0.1:9092",
		Topic:      "in",
		GroupID:    "g1",
		AutoCommit: &autoCommit,
		Balancers:  []string{"cooperative_sticky"},
	})
	if err != nil {
		t.Fatalf("new kafka receiver: %v", err)
	}
	t.Cleanup(func() { _ = r.Stop(context.Background()) })

	if got := r.client.OptValue(kgo.DisableAutoCommit).(bool); !got {
		t.Fatalf("expected auto commit disabled")
	}
}
