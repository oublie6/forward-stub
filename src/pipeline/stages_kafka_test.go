package pipeline

import (
	"testing"

	"forward-stub/src/packet"
)

func TestSetKafkaRecordKeyFromOffsetBytesText(t *testing.T) {
	st := SetKafkaRecordKeyFromOffsetBytes(2, 4, "text")
	p := &packet.Packet{Envelope: packet.Envelope{Payload: []byte("xxABCDyy")}}

	out := st([]*packet.Packet{p})
	if len(out) != 1 {
		t.Fatalf("stage should pass")
	}
	if p.Meta.KafkaRecordKey != "ABCD" {
		t.Fatalf("kafka record key got=%q want ABCD", p.Meta.KafkaRecordKey)
	}
}

func TestSetKafkaRecordKeyFromOffsetBytesHex(t *testing.T) {
	st := SetKafkaRecordKeyFromOffsetBytes(1, 3, "hex")
	p := &packet.Packet{Envelope: packet.Envelope{Payload: []byte{0x00, 0x0a, 0x1b, 0xff, 0xee}}}

	out := st([]*packet.Packet{p})
	if len(out) != 1 {
		t.Fatalf("stage should pass")
	}
	if p.Meta.KafkaRecordKey != "0a1bff" {
		t.Fatalf("kafka record key got=%q want 0a1bff", p.Meta.KafkaRecordKey)
	}
}

func TestSetKafkaRecordKeyFromOffsetBytesOutOfBoundsDrops(t *testing.T) {
	st := SetKafkaRecordKeyFromOffsetBytes(3, 4, "text")
	p := &packet.Packet{Envelope: packet.Envelope{Payload: []byte("abcd")}}

	out := st([]*packet.Packet{p})
	if len(out) != 0 {
		t.Fatalf("out-of-bounds stage should drop, got %d packets", len(out))
	}
	if p.Meta.KafkaRecordKey != "" {
		t.Fatalf("out-of-bounds stage should not set key, got %q", p.Meta.KafkaRecordKey)
	}
}

func TestSetKafkaRecordKeyFromOffsetBytesInvalidConfigDrops(t *testing.T) {
	tests := []struct {
		name     string
		offset   int
		length   int
		encoding string
	}{
		{name: "negative offset", offset: -1, length: 1, encoding: "text"},
		{name: "zero length", offset: 0, length: 0, encoding: "text"},
		{name: "unsupported encoding", offset: 0, length: 1, encoding: "base64"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := SetKafkaRecordKeyFromOffsetBytes(tt.offset, tt.length, tt.encoding)
			p := &packet.Packet{Envelope: packet.Envelope{Payload: []byte("abcd")}}

			out := st([]*packet.Packet{p})
			if len(out) != 0 {
				t.Fatalf("invalid stage should drop, got %d packets", len(out))
			}
			if p.Meta.KafkaRecordKey != "" {
				t.Fatalf("invalid stage should not set key, got %q", p.Meta.KafkaRecordKey)
			}
		})
	}
}
