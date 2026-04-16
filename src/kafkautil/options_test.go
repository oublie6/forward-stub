package kafkautil

import (
	"testing"

	"forward-stub/src/packet"
)

func TestRecordKeyUsesKafkaRecordKeySource(t *testing.T) {
	p := &packet.Packet{
		Envelope: packet.Envelope{
			Meta: packet.Meta{
				MatchKey:       "selector-key",
				KafkaRecordKey: "record-key",
			},
		},
	}

	if got := string(RecordKey(nil, "kafka_record_key", p)); got != "record-key" {
		t.Fatalf("record key got=%q want record-key", got)
	}
	if got := string(RecordKey(nil, "match_key", p)); got != "selector-key" {
		t.Fatalf("match key source got=%q want selector-key", got)
	}
}

func TestRecordKeyKafkaRecordKeyDoesNotFallbackToMatchKey(t *testing.T) {
	p := &packet.Packet{
		Envelope: packet.Envelope{
			Meta: packet.Meta{
				MatchKey: "selector-key",
			},
		},
	}

	if got := RecordKey(nil, "kafka_record_key", p); len(got) != 0 {
		t.Fatalf("kafka_record_key should not fallback to match_key, got=%q", got)
	}
}
