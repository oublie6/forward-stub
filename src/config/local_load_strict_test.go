package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSystemLocalRejectsUnknownField(t *testing.T) {
	dir := t.TempDir()
	systemPath := filepath.Join(dir, "system.json")
	if err := os.WriteFile(systemPath, []byte(`{"logging":{"level":"info"},"unknown":1}`), 0o644); err != nil {
		t.Fatalf("write system config: %v", err)
	}

	if _, err := LoadSystemLocal(systemPath); err == nil {
		t.Fatalf("expected unknown field error")
	}
}

func TestLoadBusinessLocalRejectsUnknownField(t *testing.T) {
	dir := t.TempDir()
	businessPath := filepath.Join(dir, "business.json")
	if err := os.WriteFile(businessPath, []byte(`{"version":1,"receivers":{},"senders":{},"pipelines":{},"tasks":{},"unknown":1}`), 0o644); err != nil {
		t.Fatalf("write business config: %v", err)
	}

	if _, err := LoadBusinessLocal(businessPath); err == nil {
		t.Fatalf("expected unknown field error")
	}
}

func TestLoadBusinessLocalRejectsRemovedTaskQueueSize(t *testing.T) {
	dir := t.TempDir()
	businessPath := filepath.Join(dir, "business.json")
	payload := `{
		"version":1,
		"receivers":{"r1":{"type":"udp_gnet","listen":":9000","selector":"sel"}},
		"selectors":{"sel":{"default_task_set":"ts"}},
		"task_sets":{"ts":["t1"]},
		"senders":{"s1":{"type":"tcp_gnet","remote":"127.0.0.1:9100"}},
		"pipelines":{"p1":[]},
		"tasks":{"t1":{"pipelines":["p1"],"senders":["s1"],"queue_size":8}}
	}`
	if err := os.WriteFile(businessPath, []byte(payload), 0o644); err != nil {
		t.Fatalf("write business config: %v", err)
	}

	if _, err := LoadBusinessLocal(businessPath); err == nil {
		t.Fatalf("expected removed queue_size to be rejected")
	}
}

func TestLoadSystemLocalRejectsRemovedTaskQueueSizeDefault(t *testing.T) {
	dir := t.TempDir()
	systemPath := filepath.Join(dir, "system.json")
	payload := `{"logging":{"level":"info"},"business_defaults":{"task":{"queue_size":8}}}`
	if err := os.WriteFile(systemPath, []byte(payload), 0o644); err != nil {
		t.Fatalf("write system config: %v", err)
	}

	if _, err := LoadSystemLocal(systemPath); err == nil {
		t.Fatalf("expected removed business_defaults.task.queue_size to be rejected")
	}
}
