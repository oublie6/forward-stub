package config

import "testing"

func TestValidateSFTPSenderRequiresFields(t *testing.T) {
	cfg := Config{
		Receivers: map[string]ReceiverConfig{
			"r1": {Type: "udp_gnet", Listen: "127.0.0.1:10001"},
		},
		Senders: map[string]SenderConfig{
			"s1": {Type: "sftp", Remote: "127.0.0.1:22", Username: "u", Password: "p", RemoteDir: "/out", HostKeyFingerprint: "SHA256:test"},
		},
		Pipelines: map[string][]StageConfig{"p1": {}},
		Tasks: map[string]TaskConfig{
			"t1": {Receivers: []string{"r1"}, Pipelines: []string{"p1"}, Senders: []string{"s1"}},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected sftp sender config valid, got: %v", err)
	}

	cfg.Senders["s1"] = SenderConfig{Type: "sftp", Remote: "127.0.0.1:22", Username: "u", Password: "", RemoteDir: "/out"}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected error for missing sftp sender password")
	}

	cfg.Senders["s1"] = SenderConfig{Type: "sftp", Remote: "127.0.0.1:22", Username: "u", Password: "p", RemoteDir: "/out"}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected error for missing sftp sender host key fingerprint")
	}
}
