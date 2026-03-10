package config

import "testing"

func TestApplyDefaultsPayloadPoolMaxCachedBytes(t *testing.T) {
	cfg := Config{}
	cfg.ApplyDefaults()
	if cfg.Logging.PayloadPoolMaxCachedBytes != 0 {
		t.Fatalf("expected default payload_pool_max_cached_bytes 0(unlimited), got %d", cfg.Logging.PayloadPoolMaxCachedBytes)
	}

	cfg.Logging.PayloadPoolMaxCachedBytes = -1
	cfg.ApplyDefaults()
	if cfg.Logging.PayloadPoolMaxCachedBytes != 0 {
		t.Fatalf("expected negative payload_pool_max_cached_bytes reset to 0, got %d", cfg.Logging.PayloadPoolMaxCachedBytes)
	}
}

func TestValidateRejectsNegativePayloadPoolMaxCachedBytes(t *testing.T) {
	cfg := Config{
		Logging: LoggingConfig{PayloadPoolMaxCachedBytes: -1},
		Receivers: map[string]ReceiverConfig{
			"r1": {Type: "udp_gnet", Listen: "127.0.0.1:1"},
		},
		Senders: map[string]SenderConfig{
			"s1": {Type: "tcp_gnet", Remote: "127.0.0.1:2"},
		},
		Pipelines: map[string][]StageConfig{"p1": {}},
		Tasks: map[string]TaskConfig{
			"t1": {Receivers: []string{"r1"}, Pipelines: []string{"p1"}, Senders: []string{"s1"}},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validate to reject negative payload_pool_max_cached_bytes")
	}
}
