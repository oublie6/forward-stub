package sender

import "testing"

// TestNewUDPUnicastSenderRejectsInvalidLocalIP verifies the NewUDPUnicastSenderRejectsInvalidLocalIP behavior for the sender package.
func TestNewUDPUnicastSenderRejectsInvalidLocalIP(t *testing.T) {
	_, err := NewUDPUnicastSender("s", "bad-ip", 12345, "127.0.0.1:23456", 4<<20, 1)
	if err == nil {
		t.Fatalf("expected error for invalid local ip")
	}
}
