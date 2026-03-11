package pipeline

import (
	"testing"

	"forward-stub/src/packet"
)

func TestRouteSenderByOffsetBytes(t *testing.T) {
	st := RouteSenderByOffsetBytes(1, 2, map[string]string{string([]byte{0xAA, 0xBB}): "kafka-a"}, "")
	p := &packet.Packet{Envelope: packet.Envelope{Payload: []byte{0x00, 0xAA, 0xBB, 0xCC}}}
	if !st(p) {
		t.Fatalf("route stage should pass on match")
	}
	if p.Meta.RouteSender != "kafka-a" {
		t.Fatalf("unexpected route sender: %s", p.Meta.RouteSender)
	}
}

func TestRouteSenderByOffsetBytesDefault(t *testing.T) {
	st := RouteSenderByOffsetBytes(0, 1, map[string]string{string([]byte{0x11}): "s1"}, "default-s")
	p := &packet.Packet{Envelope: packet.Envelope{Payload: []byte{0x22}}}
	if !st(p) {
		t.Fatalf("route stage should pass on default")
	}
	if p.Meta.RouteSender != "default-s" {
		t.Fatalf("unexpected default route sender: %s", p.Meta.RouteSender)
	}
}
