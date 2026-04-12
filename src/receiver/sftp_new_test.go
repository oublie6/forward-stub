package receiver

import (
	"io"
	"testing"

	"forward-stub/src/config"
)

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

func TestFileChunkEOFUsesKnownTotalSize(t *testing.T) {
	if !fileChunkEOF(4, 4, 8, nil) {
		t.Fatalf("chunk ending exactly at total_size must be marked EOF")
	}
	if !fileChunkEOF(4, 4, 10, io.EOF) {
		t.Fatalf("read EOF must be preserved")
	}
	if fileChunkEOF(4, 4, 10, nil) {
		t.Fatalf("chunk before total_size must not be marked EOF")
	}
}
