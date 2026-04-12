// Package runtime 负责维护转发运行时对象及其测试辅助逻辑。
package runtime

import (
	"testing"

	"forward-stub/src/config"
	"forward-stub/src/pipeline"
)

// TestCompilePipelinesWithStageCacheReuseBySignature 验证相同 stage 配置会复用同一份缓存函数。
func TestCompilePipelinesWithStageCacheReuseBySignature(t *testing.T) {
	st := NewStore()
	cfg := map[string][]config.StageConfig{
		"p1": {{Type: "match_offset_bytes", Offset: 0, Hex: "aabb"}},
		"p2": {{Type: "match_offset_bytes", Offset: 0, Hex: "aabb"}},
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

// TestStageCacheTaskRefsTrackAddAndRemove 验证 task 生命周期会同步更新 stage 缓存引用计数。
func TestStageCacheTaskRefsTrackAddAndRemove(t *testing.T) {
	st := NewStore()
	st.senders["s1"] = &SenderState{Name: "s1", Cfg: config.SenderConfig{Type: "tcp_gnet", Remote: "127.0.0.1:12345"}, S: &captureSender{testNamedSender: testNamedSender{name: "s1"}}}
	st.pipelines["p1"] = &CompiledPipeline{Name: "p1", P: &pipeline.Pipeline{Name: "p1"}}
	st.pipelineCfg = map[string][]config.StageConfig{"p1": {}}

	sig := `{"type":"match_offset_bytes","offset":0,"hex":"aabb"}`
	st.stageCache[sig] = &StageCacheEntry{Sig: sig, Tasks: make(map[string]struct{})}
	st.pipelineStageSigs["p1"] = []string{sig}

	if err := st.addTask("t1", config.TaskConfig{Pipelines: []string{"p1"}, Senders: []string{"s1"}, ExecutionModel: "fastpath"}, config.LoggingConfig{}, nil); err != nil {
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

func TestAddTaskRollsBackSenderRefsOnMissingSender(t *testing.T) {
	st := NewStore()
	st.senders["s1"] = &SenderState{Name: "s1", Cfg: config.SenderConfig{Type: "tcp_gnet", Remote: "127.0.0.1:12345"}, S: &captureSender{testNamedSender: testNamedSender{name: "s1"}}}
	st.pipelines["p1"] = &CompiledPipeline{Name: "p1", P: &pipeline.Pipeline{Name: "p1"}}
	st.pipelineCfg = map[string][]config.StageConfig{"p1": {}}

	err := st.addTask("t-bad", config.TaskConfig{Pipelines: []string{"p1"}, Senders: []string{"s1", "missing"}, ExecutionModel: "fastpath"}, config.LoggingConfig{}, nil)
	if err == nil {
		t.Fatalf("expected missing sender error")
	}
	if got := st.senders["s1"].Refs; got != 0 {
		t.Fatalf("sender refs should rollback on addTask failure, got %d", got)
	}
	if _, ok := st.tasks["t-bad"]; ok {
		t.Fatalf("failed task should not be registered")
	}
}
