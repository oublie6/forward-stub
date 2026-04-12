package pipeline

import (
	"testing"

	"forward-stub/src/packet"
)

func TestRouteSenderByOffsetBytes(t *testing.T) {
	st := RouteSenderByOffsetBytes(1, 2, map[string]string{string([]byte{0xAA, 0xBB}): "kafka-a"}, "")
	p := &packet.Packet{Envelope: packet.Envelope{Payload: []byte{0x00, 0xAA, 0xBB, 0xCC}}}
	out := st([]*packet.Packet{p})
	if len(out) != 1 {
		t.Fatalf("route stage should pass on match")
	}
	if p.Meta.RouteSender != "kafka-a" {
		t.Fatalf("unexpected route sender: %s", p.Meta.RouteSender)
	}
}

func TestRouteSenderByOffsetBytesDefault(t *testing.T) {
	st := RouteSenderByOffsetBytes(0, 1, map[string]string{string([]byte{0x11}): "s1"}, "default-s")
	p := &packet.Packet{Envelope: packet.Envelope{Payload: []byte{0x22}}}
	out := st([]*packet.Packet{p})
	if len(out) != 1 {
		t.Fatalf("route stage should pass on default")
	}
	if p.Meta.RouteSender != "default-s" {
		t.Fatalf("unexpected default route sender: %s", p.Meta.RouteSender)
	}
}

func BenchmarkRouteSenderByOffsetBytesHit(b *testing.B) {
	st := RouteSenderByOffsetBytes(8, 4, map[string]string{string([]byte{0xAA, 0xBB, 0xCC, 0xDD}): "target"}, "")
	p := &packet.Packet{Envelope: packet.Envelope{Payload: []byte{0, 1, 2, 3, 4, 5, 6, 7, 0xAA, 0xBB, 0xCC, 0xDD, 9, 10}}}
	in := []*packet.Packet{p}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		p.Meta.RouteSender = ""
		out := st(in)
		if len(out) != 1 || p.Meta.RouteSender != "target" {
			b.Fatalf("route mismatch")
		}
	}
}

func BenchmarkRouteSenderByOffsetBytesDefault(b *testing.B) {
	st := RouteSenderByOffsetBytes(8, 4, map[string]string{string([]byte{0xAA, 0xBB, 0xCC, 0xDD}): "target"}, "fallback")
	p := &packet.Packet{Envelope: packet.Envelope{Payload: []byte{0, 1, 2, 3, 4, 5, 6, 7, 0x10, 0x20, 0x30, 0x40, 9, 10}}}
	in := []*packet.Packet{p}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		p.Meta.RouteSender = ""
		out := st(in)
		if len(out) != 1 || p.Meta.RouteSender != "fallback" {
			b.Fatalf("route mismatch")
		}
	}
}
