package config

import "testing"

// TestSplitExampleConfigsAreValid 验证 config 包中 SplitExampleConfigsAreValid 的行为。
func TestSplitExampleConfigsAreValid(t *testing.T) {
	_, _, cfg, err := LoadLocalPair("../../configs/system.example.json", "../../configs/business.example.json")
	if err != nil {
		t.Fatalf("load split example configs: %v", err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate split example configs: %v", err)
	}
}

// TestLegacyExampleConfigIsValid 验证 config 包中 LegacyExampleConfigIsValid 的行为。
func TestLegacyExampleConfigIsValid(t *testing.T) {
	_, _, cfg, err := LoadLocalPair("../../configs/example.json", "../../configs/example.json")
	if err != nil {
		t.Fatalf("load legacy example config: %v", err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate legacy example config: %v", err)
	}
}
