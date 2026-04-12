package config

import (
	"path/filepath"
	"testing"
)

func configPath(name string) string {
	return filepath.Clean(filepath.Join("..", "..", "configs", name))
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
		{name: "oss", systemFile: "system.example.json", bizFile: "oss.business.example.json"},
		{name: "task_models", systemFile: "system.example.json", bizFile: "task-models.business.example.json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, cfg, err := LoadLocalPair(configPath(tt.systemFile), configPath(tt.bizFile))
			if err != nil {
				t.Fatalf("load config pair: %v", err)
			}
			cfg.ApplyDefaults(BusinessDefaultsConfig{})
			if err := cfg.Validate(); err != nil {
				t.Fatalf("validate config pair: %v", err)
			}
		})
	}
}
