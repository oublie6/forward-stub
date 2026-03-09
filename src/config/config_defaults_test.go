package config

import "testing"

func TestApplyDefaultsSetsConfigWatchInterval(t *testing.T) {
	cfg := Config{}
	cfg.ApplyDefaults()
	if cfg.Control.ConfigWatchInterval != DefaultConfigWatchInterval {
		t.Fatalf("unexpected config_watch_interval default: %q", cfg.Control.ConfigWatchInterval)
	}
}
