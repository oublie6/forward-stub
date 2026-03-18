package config

import "testing"

// TestValidateRouteStageTargetsInTaskSenders verifies the ValidateRouteStageTargetsInTaskSenders behavior for the config package.
func TestValidateRouteStageTargetsInTaskSenders(t *testing.T) {
	cfg := Config{
		Logging: LoggingConfig{Level: "info"},
		Receivers: map[string]ReceiverConfig{
			"r1": {Type: "udp_gnet", Listen: ":9001"},
		},
		Senders: map[string]SenderConfig{
			"s1": {Type: "tcp_gnet", Remote: "127.0.0.1:9002"},
			"s2": {Type: "tcp_gnet", Remote: "127.0.0.1:9003"},
		},
		Pipelines: map[string][]StageConfig{
			"p1": {{Type: "route_offset_bytes_sender", Offset: 0, Cases: map[string]string{"aa": "s2"}}},
		},
		Selectors: testSelectors("r1", "t1"),
		Tasks: map[string]TaskConfig{
			"t1": {Pipelines: []string{"p1"}, Senders: []string{"s1"}},
		},
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validate error for route target outside task senders")
	}
}
