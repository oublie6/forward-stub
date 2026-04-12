// Package runtime 负责维护转发运行时对象及其测试辅助逻辑。
package runtime

import (
	"strings"
	"testing"

	"forward-stub/src/config"
	"forward-stub/src/packet"
)

func TestCompilePipelineRejectsRemovedFileStages(t *testing.T) {
	tests := []config.StageConfig{
		{Type: "split_file_chunk_to_packets"},
		{Type: "stream_packets_to_file_segments"},
		{Type: "mark_as_file_chunk"},
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

func TestCompileStageRejectsInvalidFileRewriteConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.StageConfig
		want string
	}{
		{"set empty path", config.StageConfig{Type: "set_target_file_path"}, "requires value"},
		{"strip empty prefix", config.StageConfig{Type: "rewrite_target_path_strip_prefix"}, "requires prefix"},
		{"add empty prefix", config.StageConfig{Type: "rewrite_target_path_add_prefix"}, "requires prefix"},
		{"replace empty old", config.StageConfig{Type: "rewrite_target_filename_replace"}, "requires old"},
		{"path regex invalid", config.StageConfig{Type: "rewrite_target_path_regex", Pattern: "["}, "invalid pattern"},
		{"filename regex invalid", config.StageConfig{Type: "rewrite_target_filename_regex", Pattern: "["}, "invalid pattern"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := compileStage(tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("compile error got=%v want contains %q", err, tt.want)
			}
		})
	}
}

func TestCompilePipelineMixesRouteAndFileStages(t *testing.T) {
	compiled, err := CompilePipelines(map[string][]config.StageConfig{
		"p": {
			{Type: "rewrite_target_path_add_prefix", Prefix: "out"},
			{Type: "route_offset_bytes_sender", Offset: 0, Cases: map[string]string{"aa": "s-a"}, DefaultSender: "s-default"},
			{Type: "rewrite_target_filename_replace", Old: ".csv", New: ".ready"},
		},
	})
	if err != nil {
		t.Fatalf("compile mixed pipeline: %v", err)
	}

	p := &packet.Packet{Envelope: packet.Envelope{Payload: []byte{0xaa}, Meta: packet.Meta{FilePath: "in/a.csv", FileName: "a.csv"}}}
	out := compiled["p"].P.Process(p)
	if len(out) != 1 {
		t.Fatalf("unexpected output count: %d", len(out))
	}
	if got := out[0].Meta.RouteSender; got != "s-a" {
		t.Fatalf("route sender got=%q", got)
	}
	if got := out[0].Meta.TargetFilePath; got != "out/in/a.ready" {
		t.Fatalf("target path got=%q", got)
	}
}
