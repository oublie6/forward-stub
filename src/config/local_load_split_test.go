package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadSystemAndBusinessLocal 验证 config 包中 LoadSystemAndBusinessLocal 的行为。
func TestLoadSystemAndBusinessLocal(t *testing.T) {
	dir := t.TempDir()
	systemPath := filepath.Join(dir, "system.json")
	businessPath := filepath.Join(dir, "business.json")

	if err := os.WriteFile(systemPath, []byte(`{"control":{"timeout_sec":9},"logging":{"level":"warn"}}`), 0o644); err != nil {
		t.Fatalf("write system config: %v", err)
	}
	if err := os.WriteFile(businessPath, []byte(`{"version":2,"receivers":{"r1":{"type":"udp_gnet","listen":":1"}},"senders":{"s1":{"type":"tcp_gnet","remote":"127.0.0.1:2"}},"pipelines":{"p1":[]},"selectors":{"sel":{"receivers":["r1"],"tasks":["t1"]}},"tasks":{"t1":{"pipelines":["p1"],"senders":["s1"]}}}`), 0o644); err != nil {
		t.Fatalf("write business config: %v", err)
	}

	sys, err := LoadSystemLocal(systemPath)
	if err != nil {
		t.Fatalf("load system config: %v", err)
	}
	biz, err := LoadBusinessLocal(businessPath)
	if err != nil {
		t.Fatalf("load business config: %v", err)
	}

	cfg := sys.Merge(biz)
	cfg.ApplyDefaults()
	if cfg.Control.TimeoutSec != 9 {
		t.Fatalf("unexpected timeout: %d", cfg.Control.TimeoutSec)
	}
	if cfg.Version != 2 || len(cfg.Tasks) != 1 {
		t.Fatalf("unexpected merged config: version=%d tasks=%d", cfg.Version, len(cfg.Tasks))
	}
}
