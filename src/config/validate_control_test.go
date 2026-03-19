package config

import (
	"strings"
	"testing"
)

// TestValidateRejectsInvalidPprofPort 验证 config 包中 ValidateRejectsInvalidPprofPort 的行为。
func TestValidateRejectsInvalidPprofPort(t *testing.T) {
	cfg := Config{
		Control: ControlConfig{PprofPort: 70000},
		Logging: LoggingConfig{},
		Receivers: map[string]ReceiverConfig{
			"r1": {Type: "udp_gnet", Listen: ":9001"},
		},
		Senders: map[string]SenderConfig{
			"s1": {Type: "tcp_gnet", Remote: "127.0.0.1:9002"},
		},
		Pipelines: map[string][]StageConfig{"p1": {}},
		Selectors: testSelectors("r1", "t1"),
		Tasks: map[string]TaskConfig{
			"t1": {Pipelines: []string{"p1"}, Senders: []string{"s1"}},
		},
	}
	cfg.ApplyDefaults()

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "pprof_port") {
		t.Fatalf("expected pprof_port validation error, got: %v", err)
	}
}

// TestValidateAllowsDisabledPprofPort 验证 config 包中 ValidateAllowsDisabledPprofPort 的行为。
func TestValidateAllowsDisabledPprofPort(t *testing.T) {
	cfg := Config{
		Control: ControlConfig{PprofPort: -1},
		Logging: LoggingConfig{},
		Receivers: map[string]ReceiverConfig{
			"r1": {Type: "udp_gnet", Listen: ":9001"},
		},
		Senders: map[string]SenderConfig{
			"s1": {Type: "tcp_gnet", Remote: "127.0.0.1:9002"},
		},
		Pipelines: map[string][]StageConfig{"p1": {}},
		Selectors: testSelectors("r1", "t1"),
		Tasks: map[string]TaskConfig{
			"t1": {Pipelines: []string{"p1"}, Senders: []string{"s1"}},
		},
	}
	cfg.ApplyDefaults()

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected disabled pprof_port to be valid, got: %v", err)
	}
}

// TestValidateRejectsInvalidGCStatsInterval 验证 config 包中 ValidateRejectsInvalidGCStatsInterval 的行为。
func TestValidateRejectsInvalidGCStatsInterval(t *testing.T) {
	cfg := Config{
		Logging: LoggingConfig{GCStatsInterval: "not-a-duration"},
		Receivers: map[string]ReceiverConfig{
			"r1": {Type: "udp_gnet", Listen: ":9001"},
		},
		Senders: map[string]SenderConfig{
			"s1": {Type: "tcp_gnet", Remote: "127.0.0.1:9002"},
		},
		Pipelines: map[string][]StageConfig{"p1": {}},
		Selectors: testSelectors("r1", "t1"),
		Tasks: map[string]TaskConfig{
			"t1": {Pipelines: []string{"p1"}, Senders: []string{"s1"}},
		},
	}
	cfg.ApplyDefaults()
	cfg.Logging.GCStatsInterval = "not-a-duration"

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "gc_stats_interval") {
		t.Fatalf("expected gc_stats_interval validation error, got: %v", err)
	}
}

// TestValidateAllowsValidGCStatsInterval 验证 config 包中 ValidateAllowsValidGCStatsInterval 的行为。
func TestValidateAllowsValidGCStatsInterval(t *testing.T) {
	cfg := Config{
		Logging: LoggingConfig{GCStatsEnabled: true, GCStatsInterval: "10m"},
		Receivers: map[string]ReceiverConfig{
			"r1": {Type: "udp_gnet", Listen: ":9001"},
		},
		Senders: map[string]SenderConfig{
			"s1": {Type: "tcp_gnet", Remote: "127.0.0.1:9002"},
		},
		Pipelines: map[string][]StageConfig{"p1": {}},
		Selectors: testSelectors("r1", "t1"),
		Tasks: map[string]TaskConfig{
			"t1": {Pipelines: []string{"p1"}, Senders: []string{"s1"}},
		},
	}
	cfg.ApplyDefaults()
	cfg.Logging.GCStatsEnabled = true
	cfg.Logging.GCStatsInterval = "10m"

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid gc_stats_interval, got: %v", err)
	}
}
