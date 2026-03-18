package receiver

import (
	"testing"

	"forward-stub/src/config"
)

// TestNewSFTPReceiverRejectsInvalidFingerprint 验证 receiver 包中 NewSFTPReceiverRejectsInvalidFingerprint 的行为。
func TestNewSFTPReceiverRejectsInvalidFingerprint(t *testing.T) {
	_, err := NewSFTPReceiver("r1", config.ReceiverConfig{
		Listen:             "127.0.0.1:22",
		Username:           "u",
		Password:           "p",
		RemoteDir:          "/in",
		HostKeyFingerprint: "SHA256:***",
	})
	if err == nil {
		t.Fatalf("expected invalid host key fingerprint error")
	}
}
