package runtime

import (
	"testing"

	"forward-stub/src/config"
	"forward-stub/src/pipeline"
)

func TestCompilePipelinesWithStageCacheReuseBySignature(t *testing.T) {
	st := NewStore()
	cfg := map[string][]config.StageConfig{
		"p1": {{Type: "clear_file_meta"}},
		"p2": {{Type: "clear_file_meta"}},
	}

	compiled, sigsByPipeline, err := st.compilePipelinesWithStageCache(cfg)
	if err != nil {
		t.Fatalf("compile with stage cache failed: %v", err)
	}
	if len(compiled) != 2 || len(sigsByPipeline) != 2 {
		t.Fatalf("unexpected compile result: pipelines=%d sigPipelines=%d", len(compiled), len(sigsByPipeline))
	}
	if len(st.stageCache) != 1 {
		t.Fatalf("expected 1 shared stage cache entry, got %d", len(st.stageCache))
	}
}

func TestStageCacheTaskRefsTrackAddAndRemove(t *testing.T) {
	st := NewStore()
	st.senders["s1"] = &SenderState{Name: "s1", Cfg: config.SenderConfig{Type: "tcp_gnet", Remote: "127.0.0.1:12345"}, S: &captureSender{name: "s1"}}
	st.pipelines["p1"] = &CompiledPipeline{Name: "p1", P: &pipeline.Pipeline{Name: "p1"}}
	st.pipelineCfg = map[string][]config.StageConfig{"p1": {}}

	sig := `{"type":"clear_file_meta"}`
	st.stageCache[sig] = &StageCacheEntry{Sig: sig, Tasks: make(map[string]struct{})}
	st.pipelineStageSigs["p1"] = []string{sig}

	if err := st.addTask("t1", config.TaskConfig{Receivers: []string{"r1"}, Pipelines: []string{"p1"}, Senders: []string{"s1"}, ExecutionModel: "fastpath"}, config.LoggingConfig{}, nil); err != nil {
		t.Fatalf("add task failed: %v", err)
	}
	entry := st.stageCache[sig]
	if entry.TaskRefs != 1 {
		t.Fatalf("expected task refs 1 after add, got %d", entry.TaskRefs)
	}
	if _, ok := entry.Tasks["t1"]; !ok {
		t.Fatalf("expected task t1 registered in stage entry")
	}

	_ = st.removeTask("t1", false)
	if entry.TaskRefs != 0 {
		t.Fatalf("expected task refs 0 after remove, got %d", entry.TaskRefs)
	}
	if _, ok := entry.Tasks["t1"]; ok {
		t.Fatalf("expected task t1 removed from stage entry")
	}
}
