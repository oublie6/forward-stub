// Package runtime 负责维护转发运行时对象及其测试辅助逻辑。
package runtime

import (
	"testing"

	"forward-stub/src/config"
)

// TestCompilePipelineFileStages 验证文件分块相关 stage 可以被正确编译并保留执行顺序。
func TestCompilePipelineFileStages(t *testing.T) {
	eof := true
	cfg := map[string][]config.StageConfig{
		"p": {
			{Type: "mark_as_file_chunk", Path: "/tmp/a.bin", Bool: &eof},
			{Type: "clear_file_meta"},
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
