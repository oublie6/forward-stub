package config

import "testing"

func TestApplyDefaultsSetsControlDefaults(t *testing.T) {
	cfg := Config{}
	cfg.ApplyDefaults()
	if cfg.Control.ConfigWatchInterval != DefaultConfigWatchInterval {
		t.Fatalf("unexpected config_watch_interval default: %q", cfg.Control.ConfigWatchInterval)
	}
	if cfg.Control.PprofPort != DefaultPprofPort {
		t.Fatalf("unexpected pprof_port default: %d", cfg.Control.PprofPort)
	}
}

func TestApplyDefaultsNormalizesNegativePprofPort(t *testing.T) {
	cfg := Config{Control: ControlConfig{PprofPort: -1}}
	cfg.ApplyDefaults()
	if cfg.Control.PprofPort != 0 {
		t.Fatalf("unexpected pprof_port after defaults: %d", cfg.Control.PprofPort)
	}
}
