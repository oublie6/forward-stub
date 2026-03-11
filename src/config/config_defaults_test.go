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

func TestApplyDefaultsKeepsDisabledPprofPort(t *testing.T) {
	cfg := Config{Control: ControlConfig{PprofPort: -1}}
	cfg.ApplyDefaults()
	if cfg.Control.PprofPort != -1 {
		t.Fatalf("unexpected pprof_port after defaults: %d", cfg.Control.PprofPort)
	}
}

func TestApplyDefaultsSetsReceiverMulticoreWhenUnset(t *testing.T) {
	cfg := Config{Receivers: map[string]ReceiverConfig{"r1": {Type: "udp_gnet", Listen: ":9000"}}}
	cfg.ApplyDefaults()
	rc := cfg.Receivers["r1"]
	if rc.Multicore == nil || !*rc.Multicore {
		t.Fatalf("unexpected receiver multicore default: %+v", rc.Multicore)
	}
}
