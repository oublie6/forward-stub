package config

import "testing"

// baseConfigForTaskModel is a package-local helper used by validate_execution_model_test.go.
func baseConfigForTaskModel() Config {
	return Config{
		Version: 1,
		Receivers: map[string]ReceiverConfig{
			"r": {Type: "udp_gnet", Listen: "udp://127.0.0.1:19001"},
		},
		Senders: map[string]SenderConfig{
			"s": {Type: "udp_unicast", LocalIP: "127.0.0.1", LocalPort: 19002, Remote: "127.0.0.1:19003"},
		},
		Pipelines: map[string][]StageConfig{"p": {}},
		Selectors: testSelectors("r", "t"),
		Tasks: map[string]TaskConfig{
			"t": {ExecutionModel: "channel", Pipelines: []string{"p"}, Senders: []string{"s"}},
		},
	}
}

// TestValidateTaskExecutionModel verifies the ValidateTaskExecutionModel behavior for the config package.
func TestValidateTaskExecutionModel(t *testing.T) {
	cfg := baseConfigForTaskModel()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected channel model valid: %v", err)
	}

	cfg.Tasks["t"] = TaskConfig{ExecutionModel: "bad_mode", Pipelines: []string{"p"}, Senders: []string{"s"}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid execution_model error")
	}

	cfg = baseConfigForTaskModel()
	cfg.Tasks["t"] = TaskConfig{ExecutionModel: "channel", ChannelQueueSize: -1, Pipelines: []string{"p"}, Senders: []string{"s"}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid channel_queue_size error")
	}
}
