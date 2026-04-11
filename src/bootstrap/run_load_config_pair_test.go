package bootstrap

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"forward-stub/src/config"
)

func pairBusinessConfigJSON(version int, includeControl bool) string {
	control := ""
	if includeControl {
		control = `,"control":{"pprof_port":9999}`
	}
	return fmt.Sprintf(`{"version":%d,"receivers":{"r1":{"type":"udp_gnet","listen":":9001","selector":"sel1"}},"selectors":{"sel1":{"default_task_set":"ts1"}},"task_sets":{"ts1":["t1"]},"senders":{"s1":{"type":"tcp_gnet","remote":"127.0.0.1:9002"}},"pipelines":{"p1":[]},"tasks":{"t1":{"pipelines":["p1"],"senders":["s1"]}}%s}`, version, control)
}

func writePairConfigFile(t *testing.T, path string, version int, includeControl bool) {
	t.Helper()
	body := pairBusinessConfigJSON(version, includeControl)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write business config: %v", err)
	}
}

func TestLoadConfigPairAppliesDefaultsWithoutAPI(t *testing.T) {
	dir := t.TempDir()
	systemPath := filepath.Join(dir, "system.json")
	businessPath := filepath.Join(dir, "business.json")

	if err := os.WriteFile(systemPath, []byte(`{"logging":{"level":"warn"}}`), 0o644); err != nil {
		t.Fatalf("write system config: %v", err)
	}
	writePairConfigFile(t, businessPath, 1, false)

	_, _, cfg, err := loadConfigPair(context.Background(), systemPath, businessPath)
	if err != nil {
		t.Fatalf("load config pair: %v", err)
	}

	if cfg.Control.PprofPort != config.DefaultPprofPort {
		t.Fatalf("unexpected pprof default: %d", cfg.Control.PprofPort)
	}
}

func TestLoadConfigPairAppliesDefaultsAfterAPIOverride(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(pairBusinessConfigJSON(3, false)))
	}))
	defer ts.Close()

	dir := t.TempDir()
	systemPath := filepath.Join(dir, "system.json")
	businessPath := filepath.Join(dir, "business.json")

	if err := os.WriteFile(systemPath, []byte(`{"control":{"api":"`+ts.URL+`"},"logging":{"level":"info"}}`), 0o644); err != nil {
		t.Fatalf("write system config: %v", err)
	}
	writePairConfigFile(t, businessPath, 1, false)

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

func TestLoadConfigPairAppliesControlDefaultsBeforeAPIFetch(t *testing.T) {
	var gotTimeoutHeader string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTimeoutHeader = r.Header.Get("X-Test-Timeout")
		_, _ = w.Write([]byte(pairBusinessConfigJSON(3, false)))
	}))
	defer ts.Close()

	dir := t.TempDir()
	systemPath := filepath.Join(dir, "system.json")
	businessPath := filepath.Join(dir, "business.json")

	if err := os.WriteFile(systemPath, []byte(`{"control":{"api":"`+ts.URL+`"},"logging":{"level":"info"}}`), 0o644); err != nil {
		t.Fatalf("write system config: %v", err)
	}
	writePairConfigFile(t, businessPath, 1, false)

	_, _, cfg, err := loadConfigPair(context.Background(), systemPath, businessPath)
	if err != nil {
		t.Fatalf("load config pair: %v", err)
	}
	if cfg.Control.TimeoutSec != config.DefaultControlTimeoutSec {
		t.Fatalf("unexpected timeout default after api fetch: %d", cfg.Control.TimeoutSec)
	}
	if gotTimeoutHeader != "" {
		t.Fatalf("unexpected test header leak: %q", gotTimeoutHeader)
	}
}

func TestLoadConfigPairAPIOnlyBusinessAndMergeSystem(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(pairBusinessConfigJSON(9, true)))
	}))
	defer ts.Close()

	dir := t.TempDir()
	systemPath := filepath.Join(dir, "system.json")
	businessPath := filepath.Join(dir, "business.json")

	if err := os.WriteFile(systemPath, []byte(`{"control":{"api":"`+ts.URL+`","pprof_port":7001},"logging":{"level":"warn"}}`), 0o644); err != nil {
		t.Fatalf("write system config: %v", err)
	}
	writePairConfigFile(t, businessPath, 1, false)

	if _, _, _, err := loadConfigPair(context.Background(), systemPath, businessPath); err == nil {
		t.Fatalf("expected api business-only schema violation")
	}
}

func TestLoadConfigPairNormalizesFinalBusinessOnlyOnceAfterAPIFetch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(pairBusinessConfigJSON(5, false)))
	}))
	defer ts.Close()

	dir := t.TempDir()
	systemPath := filepath.Join(dir, "system.json")
	businessPath := filepath.Join(dir, "business.json")

	systemJSON := `{
		"control":{"api":"` + ts.URL + `"},
		"logging":{"level":"info"},
		"business_defaults":{"task":{"pool_size":64},"sender":{"concurrency":4}}
	}`
	if err := os.WriteFile(systemPath, []byte(systemJSON), 0o644); err != nil {
		t.Fatalf("write system config: %v", err)
	}
	localBusiness := `{"version":1,"receivers":{"r_local":{"type":"udp_gnet","listen":":9001","selector":"sel1"}},"selectors":{"sel1":{"default_task_set":"ts1"}},"task_sets":{"ts1":["t1"]},"senders":{"s1":{"type":"tcp_gnet","remote":"127.0.0.1:9002","concurrency":8}},"pipelines":{"p1":[]},"tasks":{"t1":{"pool_size":32,"pipelines":["p1"],"senders":["s1"]}}}`
	if err := os.WriteFile(businessPath, []byte(localBusiness), 0o644); err != nil {
		t.Fatalf("write business config: %v", err)
	}

	_, biz, cfg, err := loadConfigPair(context.Background(), systemPath, businessPath)
	if err != nil {
		t.Fatalf("load config pair: %v", err)
	}

	if biz.Version != 5 || cfg.Version != 5 {
		t.Fatalf("expected remote business to be final, biz=%d cfg=%d", biz.Version, cfg.Version)
	}
	if _, ok := cfg.Receivers["r_local"]; ok {
		t.Fatalf("local business should not leak into final config after api fetch")
	}
	if got := cfg.Tasks["t1"].PoolSize; got != 64 {
		t.Fatalf("final remote business should receive system business_defaults once, got pool_size=%d", got)
	}
	if got := cfg.Senders["s1"].Concurrency; got != 4 {
		t.Fatalf("final remote business should receive sender business_defaults once, got concurrency=%d", got)
	}
}
