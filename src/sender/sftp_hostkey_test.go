package sender

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"

	"golang.org/x/crypto/ssh"
)

// TestSFTPSenderHostKeyCallback verifies the SFTPSenderHostKeyCallback behavior for the sender package.
func TestSFTPSenderHostKeyCallback(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	pub, err := ssh.NewPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("new public key: %v", err)
	}
	fp := ssh.FingerprintSHA256(pub)

	s := &SFTPSender{hostKeyFingerprint: fp}
	if err := s.hostKeyCallback()("example", nil, pub); err != nil {
		t.Fatalf("expected host key match, got: %v", err)
	}

	s.hostKeyFingerprint = "SHA256:mismatch"
	if err := s.hostKeyCallback()("example", nil, pub); err == nil {
		t.Fatalf("expected host key mismatch error")
	}
}
