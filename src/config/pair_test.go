package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadLocalPairAppliesSystemBusinessDefaults 验证 config 包中 LoadLocalPairAppliesSystemBusinessDefaults 的行为。
func TestLoadLocalPairAppliesSystemBusinessDefaults(t *testing.T) {
	dir := t.TempDir()
	systemPath := filepath.Join(dir, "system.json")
	businessPath := filepath.Join(dir, "business.json")

	if err := os.WriteFile(systemPath, []byte(`{"business_defaults":{"task":{"execution_model":"pool","pool_size":64},"sender":{"concurrency":3}},"logging":{"level":"info"}}`), 0o644); err != nil {
		t.Fatalf("write system config: %v", err)
	}
	if err := os.WriteFile(businessPath, []byte(`{"version":2,"receivers":{"r1":{"type":"udp_gnet","listen":":1"}},"senders":{"s1":{"type":"tcp_gnet","remote":"127.0.0.1:2"}},"pipelines":{"p1":[]},"selectors":{"sel":{"receivers":["r1"],"tasks":["t1"]}},"tasks":{"t1":{"pipelines":["p1"],"senders":["s1"]}}}`), 0o644); err != nil {
		t.Fatalf("write business config: %v", err)
	}

	_, _, cfg, err := LoadLocalPair(systemPath, businessPath)
	if err != nil {
		t.Fatalf("load local pair: %v", err)
	}
	if cfg.Tasks["t1"].PoolSize != 64 || cfg.Tasks["t1"].ExecutionModel != "pool" {
		t.Fatalf("unexpected task defaults in merged cfg: %+v", cfg.Tasks["t1"])
	}
	if cfg.Senders["s1"].Concurrency != 3 {
		t.Fatalf("unexpected sender defaults in merged cfg: %+v", cfg.Senders["s1"])
	}
}

// TestLoadLocalPairDoesNotApplyRuntimeDefaults 验证 config 包中 LoadLocalPairDoesNotApplyRuntimeDefaults 的行为。
func TestLoadLocalPairDoesNotApplyRuntimeDefaults(t *testing.T) {
	dir := t.TempDir()
	systemPath := filepath.Join(dir, "system.json")
	businessPath := filepath.Join(dir, "business.json")

	if err := os.WriteFile(systemPath, []byte(`{"logging":{"level":"info"}}`), 0o644); err != nil {
		t.Fatalf("write system config: %v", err)
	}
	if err := os.WriteFile(businessPath, []byte(`{"version":2,"receivers":{"r1":{"type":"udp_gnet","listen":":1"}},"senders":{"s1":{"type":"tcp_gnet","remote":"127.0.0.1:2"}},"pipelines":{"p1":[]},"selectors":{"sel":{"receivers":["r1"],"tasks":["t1"]}},"tasks":{"t1":{"pipelines":["p1"],"senders":["s1"]}}}`), 0o644); err != nil {
		t.Fatalf("write business config: %v", err)
	}

	_, _, cfg, err := LoadLocalPair(systemPath, businessPath)
	if err != nil {
		t.Fatalf("load local pair: %v", err)
	}
	if cfg.Control.PprofPort != 0 {
		t.Fatalf("load local pair should not apply runtime defaults, got pprof_port=%d", cfg.Control.PprofPort)
	}
}
