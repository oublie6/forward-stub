package sender

import (
	"testing"

	"forward-stub/src/config"
)

// TestNewSFTPSenderRejectsInvalidFingerprint 验证 sender 包中 NewSFTPSenderRejectsInvalidFingerprint 的行为。
func TestNewSFTPSenderRejectsInvalidFingerprint(t *testing.T) {
	_, err := NewSFTPSender("s1", config.SenderConfig{
		Remote:             "127.0.0.1:22",
		Username:           "u",
		Password:           "p",
		RemoteDir:          "/out",
		HostKeyFingerprint: "SHA256:***",
	})
	if err == nil {
		t.Fatalf("expected invalid host key fingerprint error")
	}
}
