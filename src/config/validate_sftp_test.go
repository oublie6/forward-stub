package config

import "testing"

func TestValidateSFTPReceiverRequiresFields(t *testing.T) {
	cfg := Config{
		Receivers: map[string]ReceiverConfig{
			"r1": {Type: "sftp", Listen: "127.0.0.1:22", Username: "u", Password: "p", RemoteDir: "/in", HostKeyFingerprint: "SHA256:W5M5Qf3jQ8jD8I2LqzY9zT6QfPj1O9g3k8xw0Jm9r3A"},
		},
		Senders: map[string]SenderConfig{
			"s1": {Type: "udp_unicast", Remote: "127.0.0.1:9000", LocalPort: 9001},
		},
		Pipelines: map[string][]StageConfig{"p1": {}},
		Tasks: map[string]TaskConfig{
			"t1": {Receivers: []string{"r1"}, Pipelines: []string{"p1"}, Senders: []string{"s1"}},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected sftp config valid, got: %v", err)
	}

	cfg.Receivers["r1"] = ReceiverConfig{Type: "sftp", Listen: "127.0.0.1:22", Username: "u", Password: "", RemoteDir: "/in"}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected error for missing sftp password")
	}

	cfg.Receivers["r1"] = ReceiverConfig{Type: "sftp", Listen: "127.0.0.1:22", Username: "u", Password: "p", RemoteDir: "/in"}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected error for missing sftp host key fingerprint")
	}

	cfg.Receivers["r1"] = ReceiverConfig{Type: "sftp", Listen: "127.0.0.1:22", Username: "u", Password: "p", RemoteDir: "/in", HostKeyFingerprint: "SHA256:***"}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected error for invalid sftp host key fingerprint")
	}
}
