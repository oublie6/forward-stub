package config

import (
	"strings"
	"testing"
)

func validSelectorConfig() Config {
	cfg := Config{
		Receivers: map[string]ReceiverConfig{
			"r1": {Type: "udp_gnet", Listen: ":9001", Selector: "sel1"},
		},
		Selectors: map[string]SelectorConfig{
			"sel1": {
				Matches:        map[string]string{"udp|src_addr=1.1.1.1:9000": "ts1"},
				DefaultTaskSet: "ts1",
			},
		},
		TaskSets: map[string][]string{
			"ts1": []string{"t1"},
		},
		Senders: map[string]SenderConfig{
			"s1": {Type: "tcp_gnet", Remote: "127.0.0.1:9002"},
		},
		Pipelines: map[string][]StageConfig{"p1": {}},
		Tasks: map[string]TaskConfig{
			"t1": {Pipelines: []string{"p1"}, Senders: []string{"s1"}},
		},
	}
	cfg.ApplyDefaults()
	return cfg
}

func TestValidateSelectorReferences(t *testing.T) {
	cfg := validSelectorConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected selector config valid: %v", err)
	}

	cfg = validSelectorConfig()
	cfg.Receivers["r1"] = ReceiverConfig{Type: "udp_gnet", Listen: ":9001", Selector: "missing"}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "selector missing not found") {
		t.Fatalf("expected missing selector error, got: %v", err)
	}

	cfg = validSelectorConfig()
	cfg.Selectors["sel1"] = SelectorConfig{Matches: map[string]string{"udp|src_addr=1.1.1.1:9000": "missing"}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "task set missing not found") {
		t.Fatalf("expected missing task set error, got: %v", err)
	}

	cfg = validSelectorConfig()
	cfg.TaskSets["ts1"] = []string{"missing-task"}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "task set ts1 task missing-task not found") {
		t.Fatalf("expected missing task error, got: %v", err)
	}
}
