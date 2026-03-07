package sender

import (
	"testing"

	"forward-stub/src/config"
)

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
