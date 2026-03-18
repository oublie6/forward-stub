package config

import (
	"strings"
	"testing"
)

// TestValidateSenderConcurrencyMustBePowerOfTwo 验证 config 包中 ValidateSenderConcurrencyMustBePowerOfTwo 的行为。
func TestValidateSenderConcurrencyMustBePowerOfTwo(t *testing.T) {
	cfg := Config{
		Logging: LoggingConfig{Level: "info"},
		Receivers: map[string]ReceiverConfig{
			"r1": {Type: "udp_gnet", Listen: "127.0.0.1:10001"},
		},
		Senders: map[string]SenderConfig{
			"s1": {Type: "tcp_gnet", Remote: "127.0.0.1:9002", Concurrency: 3},
		},
		Pipelines: map[string][]StageConfig{"p1": {}},
		Selectors: testSelectors("r1", "t1"),
		Tasks: map[string]TaskConfig{
			"t1": {Pipelines: []string{"p1"}, Senders: []string{"s1"}},
		},
	}

	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "power of two") {
		t.Fatalf("expected power-of-two concurrency error, got: %v", err)
	}
}

// TestValidateSenderConcurrencyAcceptsPowerOfTwo 验证 config 包中 ValidateSenderConcurrencyAcceptsPowerOfTwo 的行为。
func TestValidateSenderConcurrencyAcceptsPowerOfTwo(t *testing.T) {
	cfg := Config{
		Logging: LoggingConfig{Level: "info"},
		Receivers: map[string]ReceiverConfig{
			"r1": {Type: "udp_gnet", Listen: "127.0.0.1:10001"},
		},
		Senders: map[string]SenderConfig{
			"s1": {Type: "tcp_gnet", Remote: "127.0.0.1:9002", Concurrency: 8},
		},
		Pipelines: map[string][]StageConfig{"p1": {}},
		Selectors: testSelectors("r1", "t1"),
		Tasks: map[string]TaskConfig{
			"t1": {Pipelines: []string{"p1"}, Senders: []string{"s1"}},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}
}
