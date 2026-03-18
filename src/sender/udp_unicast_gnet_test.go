package sender

import (
	"context"
	"net"
	"testing"

	"forward-stub/src/packet"
)

// TestUDPUnicastSenderAllowsSameLocalPortAcrossDifferentRemotes verifies the UDPUnicastSenderAllowsSameLocalPortAcrossDifferentRemotes behavior for the sender package.
func TestUDPUnicastSenderAllowsSameLocalPortAcrossDifferentRemotes(t *testing.T) {
	ln1, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen remote1: %v", err)
	}
	defer ln1.Close()

	ln2, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen remote2: %v", err)
	}
	defer ln2.Close()

	r1 := ln1.LocalAddr().String()
	r2 := ln2.LocalAddr().String()

	portLn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve local port: %v", err)
	}
	localPort := portLn.LocalAddr().(*net.UDPAddr).Port
	_ = portLn.Close()

	s1, err := NewUDPUnicastSender("s1", "127.0.0.1", localPort, r1, 4<<20, 1)
	if err != nil {
		t.Fatalf("new sender1: %v", err)
	}
	defer s1.Close(context.Background())

	s2, err := NewUDPUnicastSender("s2", "127.0.0.1", localPort, r2, 4<<20, 1)
	if err != nil {
		t.Fatalf("new sender2 with same local port: %v", err)
	}
	defer s2.Close(context.Background())

	if err := s1.Send(context.Background(), dummyPacket("one")); err != nil {
		t.Fatalf("sender1 send: %v", err)
	}
	if err := s2.Send(context.Background(), dummyPacket("two")); err != nil {
		t.Fatalf("sender2 send: %v", err)
	}
}

// dummyPacket is a package-local helper used by udp_unicast_gnet_test.go.
func dummyPacket(payload string) *packet.Packet {
	return &packet.Packet{Envelope: packet.Envelope{Payload: []byte(payload)}}
}
