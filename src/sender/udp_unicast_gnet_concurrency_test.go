package sender

import (
	"context"
	"net"
	"testing"
	"time"

	"forward-stub/src/packet"
)

// TestUDPUnicastSenderWithConcurrencySendsPackets verifies the UDPUnicastSenderWithConcurrencySendsPackets behavior for the sender package.
func TestUDPUnicastSenderWithConcurrencySendsPackets(t *testing.T) {
	ln, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen sink: %v", err)
	}
	defer ln.Close()

	s, err := NewUDPUnicastSender("s", "127.0.0.1", 0, ln.LocalAddr().String(), 4<<20, 4)
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}
	defer s.Close(context.Background())

	recv := make(chan []byte, 4)
	go func() {
		buf := make([]byte, 2048)
		for i := 0; i < 4; i++ {
			_ = ln.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, _, err := ln.ReadFrom(buf)
			if err != nil {
				return
			}
			cp := make([]byte, n)
			copy(cp, buf[:n])
			recv <- cp
		}
	}()

	for i := 0; i < 4; i++ {
		p := &packet.Packet{Envelope: packet.Envelope{Payload: []byte{byte(i), byte(i + 1)}}}
		if err := s.Send(context.Background(), p); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}

	for i := 0; i < 4; i++ {
		select {
		case <-recv:
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting udp packet %d", i)
		}
	}
}
