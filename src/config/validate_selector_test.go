package config

import (
	"strings"
	"testing"
)

func baseConfigForSelectorValidation() Config {
	return Config{
		Logging: LoggingConfig{Level: "info"},
		Receivers: map[string]ReceiverConfig{
			"r1": {Type: "udp_gnet", Listen: ":10001"},
		},
		Senders: map[string]SenderConfig{
			"s1": {Type: "tcp_gnet", Remote: "127.0.0.1:9001"},
		},
		Pipelines: map[string][]StageConfig{"p1": {}},
		Tasks: map[string]TaskConfig{
			"t1": {Pipelines: []string{"p1"}, Senders: []string{"s1"}},
		},
		Selectors: map[string]SelectorConfig{
			"sel": {Receivers: []string{"r1"}, Tasks: []string{"t1"}},
		},
	}
}

func TestValidateSelectorConfigAndDedupes(t *testing.T) {
	cfg := baseConfigForSelectorValidation()
	cfg.Selectors["sel"] = SelectorConfig{
		Receivers: []string{"r1", "r1"},
		Tasks:     []string{"t1", "t1"},
		Source: &SourceSelectorConfig{
			SrcCIDRs:      []string{"10.0.0.1", "10.0.0.1/32", "10.0.0.0/24"},
			SrcPortRanges: []string{"8080", "8080", "8000-8002"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected selector config valid: %v", err)
	}
	got := cfg.Selectors["sel"]
	if len(got.Receivers) != 1 || got.Receivers[0] != "r1" {
		t.Fatalf("expected receivers deduped, got=%v", got.Receivers)
	}
	if len(got.Tasks) != 1 || got.Tasks[0] != "t1" {
		t.Fatalf("expected tasks deduped, got=%v", got.Tasks)
	}
	if len(got.Source.SrcCIDRs) != 2 {
		t.Fatalf("expected normalized cidrs deduped, got=%v", got.Source.SrcCIDRs)
	}
	if len(got.Source.SrcPortRanges) != 2 {
		t.Fatalf("expected normalized ports deduped, got=%v", got.Source.SrcPortRanges)
	}
}

func TestValidateSelectorAllowsSingleDefaultPerReceiver(t *testing.T) {
	cfg := baseConfigForSelectorValidation()
	cfg.Selectors = map[string]SelectorConfig{
		"default": {Receivers: []string{"r1"}, Tasks: []string{"t1"}},
		"src": {
			Receivers: []string{"r1"},
			Tasks:     []string{"t1"},
			Source:    &SourceSelectorConfig{SrcCIDRs: []string{"10.0.0.1"}},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected single default selector to be valid: %v", err)
	}
}

func TestValidateSelectorRejectsMultipleDefaultsPerReceiver(t *testing.T) {
	cfg := baseConfigForSelectorValidation()
	cfg.Selectors = map[string]SelectorConfig{
		"default-a": {Receivers: []string{"r1"}, Tasks: []string{"t1"}},
		"default-b": {Receivers: []string{"r1"}, Tasks: []string{"t1"}},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validate to fail for duplicate default selectors")
	}
	if !strings.Contains(err.Error(), "receiver r1 has multiple default selectors") || !strings.Contains(err.Error(), "default-a") || !strings.Contains(err.Error(), "default-b") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateSelectorRejectsInvalidCases(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name: "missing receivers",
			mutate: func(cfg *Config) {
				cfg.Selectors["sel"] = SelectorConfig{Tasks: []string{"t1"}}
			},
			wantErr: "has no receivers",
		},
		{
			name: "missing tasks",
			mutate: func(cfg *Config) {
				cfg.Selectors["sel"] = SelectorConfig{Receivers: []string{"r1"}}
			},
			wantErr: "has no tasks",
		},
		{
			name: "unknown receiver",
			mutate: func(cfg *Config) {
				cfg.Selectors["sel"] = SelectorConfig{Receivers: []string{"rx"}, Tasks: []string{"t1"}}
			},
			wantErr: "receiver rx not found",
		},
		{
			name: "unknown task",
			mutate: func(cfg *Config) {
				cfg.Selectors["sel"] = SelectorConfig{Receivers: []string{"r1"}, Tasks: []string{"tx"}}
			},
			wantErr: "task tx not found",
		},
		{
			name: "empty source",
			mutate: func(cfg *Config) {
				cfg.Selectors["sel"] = SelectorConfig{Receivers: []string{"r1"}, Tasks: []string{"t1"}, Source: &SourceSelectorConfig{}}
			},
			wantErr: "source is empty",
		},
		{
			name: "bad cidr",
			mutate: func(cfg *Config) {
				cfg.Selectors["sel"] = SelectorConfig{Receivers: []string{"r1"}, Tasks: []string{"t1"}, Source: &SourceSelectorConfig{SrcCIDRs: []string{"10.0.0.999"}}}
			},
			wantErr: "src_cidrs",
		},
		{
			name: "bad port range",
			mutate: func(cfg *Config) {
				cfg.Selectors["sel"] = SelectorConfig{Receivers: []string{"r1"}, Tasks: []string{"t1"}, Source: &SourceSelectorConfig{SrcPortRanges: []string{"9000-8000"}}}
			},
			wantErr: "src_port_ranges",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseConfigForSelectorValidation()
			tc.mutate(&cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}
