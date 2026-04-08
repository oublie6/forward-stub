// Package runtime 负责维护转发运行时对象及其测试辅助逻辑。
package runtime

import (
	"testing"

	"forward-stub/src/config"
)

// TestCompilePipelineFileStages 验证当前保留的 stream/file 转换 stage 可以被正确编译并保留执行顺序。
func TestCompilePipelineFileStages(t *testing.T) {
	cfg := map[string][]config.StageConfig{
		"p": {
			{Type: "split_file_chunk_to_packets", PacketSize: 1024},
			{Type: "stream_packets_to_file_segments", SegmentSize: 4096, ChunkSize: 1024, FilePrefix: "stream"},
		},
	}
	compiled, err := CompilePipelines(cfg)
	if err != nil {
		t.Fatalf("compile pipelines failed: %v", err)
	}
	if compiled["p"] == nil || len(compiled["p"].P.Stages) != 2 {
		t.Fatalf("unexpected compiled pipeline: %+v", compiled["p"])
	}
}

func TestCompilePipelineRejectsRemovedFileStages(t *testing.T) {
	tests := []config.StageConfig{
		{Type: "mark_as_file_chunk", Path: "/tmp/a.bin"},
		{Type: "clear_file_meta"},
	}
	for _, sc := range tests {
		if _, err := compileStage(sc); err == nil {
			t.Fatalf("expected removed stage %q to fail", sc.Type)
		}
	}
}
