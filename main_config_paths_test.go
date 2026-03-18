package main

import (
	"forward-stub/src/config"
	"testing"
)

// TestResolveConfigPaths verifies the ResolveConfigPaths behavior for the  package.
func TestResolveConfigPaths(t *testing.T) {
	tests := []struct {
		name       string
		legacy     string
		system     string
		business   string
		wantSystem string
		wantBiz    string
		wantErr    bool
	}{
		{name: "legacy", legacy: "cfg.json", wantSystem: "cfg.json", wantBiz: "cfg.json"},
		{name: "split", system: "sys.json", business: "biz.json", wantSystem: "sys.json", wantBiz: "biz.json"},
		{name: "missing business", system: "sys.json", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sys, biz, err := config.ResolveConfigPaths(tt.legacy, tt.system, tt.business)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolve error: %v", err)
			}
			if sys != tt.wantSystem || biz != tt.wantBiz {
				t.Fatalf("unexpected paths: sys=%s biz=%s", sys, biz)
			}
		})
	}
}
