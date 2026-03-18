package receiver

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"

	"forward-stub/src/config"

	"golang.org/x/crypto/ssh"
)

// TestSFTPReceiverHostKeyCallback 验证 receiver 包中 SFTPReceiverHostKeyCallback 的行为。
func TestSFTPReceiverHostKeyCallback(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	pub, err := ssh.NewPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("new public key: %v", err)
	}
	fp := ssh.FingerprintSHA256(pub)

	r := &SFTPReceiver{cfg: config.ReceiverConfig{HostKeyFingerprint: fp}}
	if err := r.hostKeyCallback()("example", nil, pub); err != nil {
		t.Fatalf("expected host key match, got: %v", err)
	}

	r.cfg.HostKeyFingerprint = "SHA256:mismatch"
	if err := r.hostKeyCallback()("example", nil, pub); err == nil {
		t.Fatalf("expected host key mismatch error")
	}
}
