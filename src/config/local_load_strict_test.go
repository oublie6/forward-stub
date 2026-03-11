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
