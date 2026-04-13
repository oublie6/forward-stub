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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sys, _, cfg, err := LoadLocalPair(configPath(tt.systemFile), configPath(tt.bizFile))
			if err != nil {
				t.Fatalf("load config pair: %v", err)
			}
			cfg.ApplyDefaults(sys.BusinessDefaults)
			if err := cfg.Validate(); err != nil {
				t.Fatalf("validate config pair: %v", err)
			}
		})
	}
}
