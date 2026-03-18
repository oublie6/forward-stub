package app

import (
	"testing"

	"forward-stub/src/config"
)

// TestSystemConfigChangeRequiresRestart verifies the SystemConfigChangeRequiresRestart behavior for the app package.
func TestSystemConfigChangeRequiresRestart(t *testing.T) {
	rt := NewRuntime()
	base := config.SystemConfig{Logging: config.LoggingConfig{Level: "info"}}
	if err := rt.SeedSystemConfig(base); err != nil {
		t.Fatalf("seed base system config: %v", err)
	}
	if err := rt.CheckSystemConfigStable(base); err != nil {
		t.Fatalf("check stable: %v", err)
	}
	changed := config.SystemConfig{Logging: config.LoggingConfig{Level: "debug"}}
	if err := rt.CheckSystemConfigStable(changed); err == nil {
		t.Fatalf("expected changed system config to be rejected")
	}
}
