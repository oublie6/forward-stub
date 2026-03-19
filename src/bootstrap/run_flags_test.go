package bootstrap

import "testing"

// TestRunRejectsRemovedVersionFlag 验证 bootstrap 包中 RunRejectsRemovedVersionFlag 的行为。
func TestRunRejectsRemovedVersionFlag(t *testing.T) {
	if code := Run([]string{"-version"}); code != 2 {
		t.Fatalf("expected unknown removed flag to return 2, got %d", code)
	}
}
