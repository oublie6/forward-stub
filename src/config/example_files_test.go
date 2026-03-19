package config

import (
	"path/filepath"
	"testing"
)

func configPath(name string) string {
	return filepath.Clean(filepath.Join("..", "..", "configs", name))
}

func TestExampleSingleFileConfigParsesAndValidates(t *testing.T) {
	cfg, err := LoadLocal(configPath("example.json"))
	if err != nil {
		t.Fatalf("load single example config: %v", err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate single example config: %v", err)
	}
}

func TestExampleSystemBusinessPairsParseAndValidate(t *testing.T) {
	tests := []struct {
		name       string
		systemFile string
		bizFile    string
	}{
		{name: "full", systemFile: "system.example.json", bizFile: "business.example.json"},
		{name: "minimal", systemFile: "minimal.system.example.json", bizFile: "minimal.business.example.json"},
		{name: "udp_tcp", systemFile: "system.example.json", bizFile: "udp-tcp.business.example.json"},
		{name: "kafka", systemFile: "system.example.json", bizFile: "kafka.business.example.json"},
		{name: "sftp", systemFile: "system.example.json", bizFile: "sftp.business.example.json"},
		{name: "task_models", systemFile: "system.example.json", bizFile: "task-models.business.example.json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, cfg, err := LoadLocalPair(configPath(tt.systemFile), configPath(tt.bizFile))
			if err != nil {
				t.Fatalf("load config pair: %v", err)
			}
			cfg.ApplyDefaults()
			if err := cfg.Validate(); err != nil {
				t.Fatalf("validate config pair: %v", err)
			}
		})
	}
}
