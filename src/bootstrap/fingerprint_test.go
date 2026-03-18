package bootstrap

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReadConfigFingerprintChangesWithContent verifies the ReadConfigFingerprintChangesWithContent behavior for the bootstrap package.
func TestReadConfigFingerprintChangesWithContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatalf("write initial config failed: %v", err)
	}

	fp1, err := readConfigFingerprint(path)
	if err != nil {
		t.Fatalf("read initial fingerprint failed: %v", err)
	}

	if err := os.WriteFile(path, []byte(`{"version":2}`), 0o644); err != nil {
		t.Fatalf("write updated config failed: %v", err)
	}

	fp2, err := readConfigFingerprint(path)
	if err != nil {
		t.Fatalf("read updated fingerprint failed: %v", err)
	}

	if fp1 == fp2 {
		t.Fatalf("expected different fingerprints after content change")
	}
}
