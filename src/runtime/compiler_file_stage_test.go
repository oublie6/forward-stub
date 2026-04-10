// Package runtime 负责维护转发运行时对象及其测试辅助逻辑。
package runtime

import (
	"strings"
	"testing"

	"forward-stub/src/config"
)

func TestCompilePipelineRejectsRemovedFileStages(t *testing.T) {
	tests := []config.StageConfig{
		{Type: "split_file_chunk_to_packets"},
		{Type: "stream_packets_to_file_segments"},
		{Type: "mark_as_file_chunk", Path: "/tmp/a.bin"},
		{Type: "clear_file_meta"},
	}
	for _, sc := range tests {
		if _, err := compileStage(sc); err == nil {
			t.Fatalf("expected removed stage %q to fail", sc.Type)
		} else if !strings.Contains(err.Error(), "unknown stage type") {
			t.Fatalf("expected unknown stage type error for %q, got %v", sc.Type, err)
		}
	}
}
