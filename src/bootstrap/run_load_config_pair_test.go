package bootstrap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"forward-stub/src/config"
)

// TestLoadConfigPairAppliesDefaultsWithoutAPI verifies the LoadConfigPairAppliesDefaultsWithoutAPI behavior for the bootstrap package.
func TestLoadConfigPairAppliesDefaultsWithoutAPI(t *testing.T) {
	dir := t.TempDir()
	systemPath := filepath.Join(dir, "system.json")
	businessPath := filepath.Join(dir, "business.json")

	if err := os.WriteFile(systemPath, []byte(`{"logging":{"level":"warn"}}`), 0o644); err != nil {
		t.Fatalf("write system config: %v", err)
	}
	if err := os.WriteFile(businessPath, []byte(`{"version":1,"receivers":{"r1":{"type":"udp_gnet","listen":":9001"}},"senders":{"s1":{"type":"tcp_gnet","remote":"127.0.0.1:9002"}},"pipelines":{"p1":[]},"selectors":{"sel":{"receivers":["r1"],"tasks":["t1"]}},"tasks":{"t1":{"pipelines":["p1"],"senders":["s1"]}}}`), 0o644); err != nil {
		t.Fatalf("write business config: %v", err)
	}

	_, _, cfg, err := loadConfigPair(context.Background(), systemPath, businessPath)
	if err != nil {
		t.Fatalf("load config pair: %v", err)
	}

	if cfg.Control.PprofPort != config.DefaultPprofPort {
		t.Fatalf("unexpected pprof default: %d", cfg.Control.PprofPort)
	}
}

// TestLoadConfigPairAppliesDefaultsAfterAPIOverride verifies the LoadConfigPairAppliesDefaultsAfterAPIOverride behavior for the bootstrap package.
func TestLoadConfigPairAppliesDefaultsAfterAPIOverride(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":3,"receivers":{"r1":{"type":"udp_gnet","listen":":9001"}},"senders":{"s1":{"type":"tcp_gnet","remote":"127.0.0.1:9002"}},"pipelines":{"p1":[]},"selectors":{"sel":{"receivers":["r1"],"tasks":["t1"]}},"tasks":{"t1":{"pipelines":["p1"],"senders":["s1"]}}}`))
	}))
	defer ts.Close()

	dir := t.TempDir()
	systemPath := filepath.Join(dir, "system.json")
	businessPath := filepath.Join(dir, "business.json")

	if err := os.WriteFile(systemPath, []byte(`{"control":{"api":"`+ts.URL+`"},"logging":{"level":"info"}}`), 0o644); err != nil {
		t.Fatalf("write system config: %v", err)
	}
	if err := os.WriteFile(businessPath, []byte(`{"version":1,"receivers":{"r1":{"type":"udp_gnet","listen":":9001"}},"senders":{"s1":{"type":"tcp_gnet","remote":"127.0.0.1:9002"}},"pipelines":{"p1":[]},"selectors":{"sel":{"receivers":["r1"],"tasks":["t1"]}},"tasks":{"t1":{"pipelines":["p1"],"senders":["s1"]}}}`), 0o644); err != nil {
		t.Fatalf("write business config: %v", err)
	}

	_, _, cfg, err := loadConfigPair(context.Background(), systemPath, businessPath)
	if err != nil {
		t.Fatalf("load config pair: %v", err)
	}

	if cfg.Control.PprofPort != config.DefaultPprofPort {
		t.Fatalf("unexpected pprof default after api fetch: %d", cfg.Control.PprofPort)
	}
	if cfg.Logging.Level != "info" {
		t.Fatalf("api mode should keep local system logging level, got: %s", cfg.Logging.Level)
	}
}

// TestLoadConfigPairAPIOnlyBusinessAndMergeSystem verifies the LoadConfigPairAPIOnlyBusinessAndMergeSystem behavior for the bootstrap package.
func TestLoadConfigPairAPIOnlyBusinessAndMergeSystem(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":9,"receivers":{"r1":{"type":"udp_gnet","listen":":9001"}},"senders":{"s1":{"type":"tcp_gnet","remote":"127.0.0.1:9002"}},"pipelines":{"p1":[]},"selectors":{"sel":{"receivers":["r1"],"tasks":["t1"]}},"tasks":{"t1":{"pipelines":["p1"],"senders":["s1"]}},"control":{"pprof_port":9999}}`))
	}))
	defer ts.Close()

	dir := t.TempDir()
	systemPath := filepath.Join(dir, "system.json")
	businessPath := filepath.Join(dir, "business.json")

	if err := os.WriteFile(systemPath, []byte(`{"control":{"api":"`+ts.URL+`","pprof_port":7001},"logging":{"level":"warn"}}`), 0o644); err != nil {
		t.Fatalf("write system config: %v", err)
	}
	if err := os.WriteFile(businessPath, []byte(`{"version":1,"receivers":{"r1":{"type":"udp_gnet","listen":":9001"}},"senders":{"s1":{"type":"tcp_gnet","remote":"127.0.0.1:9002"}},"pipelines":{"p1":[]},"selectors":{"sel":{"receivers":["r1"],"tasks":["t1"]}},"tasks":{"t1":{"pipelines":["p1"],"senders":["s1"]}}}`), 0o644); err != nil {
		t.Fatalf("write business config: %v", err)
	}

	if _, _, _, err := loadConfigPair(context.Background(), systemPath, businessPath); err == nil {
		t.Fatalf("expected api business-only schema violation")
	}
}
