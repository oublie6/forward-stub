package runtime

import (
	"context"
	"fmt"
	"testing"

	"forward-stub/src/config"
	"forward-stub/src/pipeline"
)

func TestApplyTaskDeltaAddUpdateRemove(t *testing.T) {
	st := NewStore()
	st.senders["s1"] = &SenderState{Name: "s1", Cfg: config.SenderConfig{Type: "tcp_gnet", Remote: "127.0.0.1:12345"}, S: &captureSender{name: "s1"}}
	st.pipelines["p1"] = &CompiledPipeline{Name: "p1", P: &pipeline.Pipeline{Name: "p1"}}
	st.pipelineCfg = map[string][]config.StageConfig{"p1": {}}
	st.subs["r1"] = map[string]struct{}{}

	if err := st.addTask("t1", config.TaskConfig{Receivers: []string{"r1"}, Pipelines: []string{"p1"}, Senders: []string{"s1"}, ExecutionModel: "fastpath"}, config.LoggingConfig{}, nil); err != nil {
		t.Fatalf("add initial task: %v", err)
	}

	cfg := config.Config{
		Version:   2,
		Receivers: map[string]config.ReceiverConfig{"r1": {Type: "udp_gnet", Listen: ":1"}},
		Senders:   map[string]config.SenderConfig{"s1": {Type: "tcp_gnet", Remote: "127.0.0.1:12345"}},
		Pipelines: map[string][]config.StageConfig{"p1": {}},
		Tasks: map[string]config.TaskConfig{
			"t1": {Receivers: []string{"r1"}, Pipelines: []string{"p1"}, Senders: []string{"s1"}, ExecutionModel: "pool", QueueSize: 1024},
			"t2": {Receivers: []string{"r1"}, Pipelines: []string{"p1"}, Senders: []string{"s1"}, ExecutionModel: "fastpath"},
		},
		Logging: config.LoggingConfig{},
	}

	if err := st.applyBusinessDelta(context.Background(), cfg); err != nil {
		t.Fatalf("apply delta: %v", err)
	}
	if len(st.tasks) != 2 {
		t.Fatalf("expected 2 tasks after delta, got %d", len(st.tasks))
	}
	if st.tasks["t1"].T.ExecutionModel != "pool" {
		t.Fatalf("expected t1 updated execution model pool, got %s", st.tasks["t1"].T.ExecutionModel)
	}

	cfg2 := cfg
	cfg2.Tasks = map[string]config.TaskConfig{
		"t2": cfg.Tasks["t2"],
	}
	if err := st.applyBusinessDelta(context.Background(), cfg2); err != nil {
		t.Fatalf("apply delta remove: %v", err)
	}
	if len(st.tasks) != 1 || st.tasks["t2"] == nil {
		t.Fatalf("expected only t2 remains, got tasks=%v", st.taskSnapshot())
	}
}

func TestRemoveTaskRefreshDispatchSnapshotImmediately(t *testing.T) {
	st := NewStore()
	st.senders["s1"] = &SenderState{Name: "s1", Cfg: config.SenderConfig{Type: "tcp_gnet", Remote: "127.0.0.1:12345"}, S: &captureSender{name: "s1"}}
	st.pipelines["p1"] = &CompiledPipeline{Name: "p1", P: &pipeline.Pipeline{Name: "p1"}}
	st.pipelineCfg = map[string][]config.StageConfig{"p1": {}}

	if err := st.addTask("t1", config.TaskConfig{Receivers: []string{"r1"}, Pipelines: []string{"p1"}, Senders: []string{"s1"}, ExecutionModel: "fastpath"}, config.LoggingConfig{}, nil); err != nil {
		t.Fatalf("add task: %v", err)
	}
	if got := len(st.getDispatchTasks("r1")); got != 1 {
		t.Fatalf("expected dispatch snapshot has 1 task, got %d", got)
	}

	_ = st.removeTask("t1", false)
	if got := len(st.getDispatchTasks("r1")); got != 0 {
		t.Fatalf("expected dispatch snapshot has 0 task after remove, got %d", got)
	}
}

func TestApplyBusinessDeltaRebuildsTasksForChangedSenderConfig(t *testing.T) {
	st := NewStore()
	st.pipelines["p1"] = &CompiledPipeline{Name: "p1", P: &pipeline.Pipeline{Name: "p1"}}
	st.pipelineCfg = map[string][]config.StageConfig{"p1": {}}
	st.subs["r1"] = map[string]struct{}{}
	st.senders["s1"] = &SenderState{Name: "s1", Cfg: config.SenderConfig{Type: "udp_unicast", LocalPort: 39001, Remote: "127.0.0.1:9"}, S: &captureSender{name: "old-s1"}}

	if err := st.addTask("t1", config.TaskConfig{Receivers: []string{"r1"}, Pipelines: []string{"p1"}, Senders: []string{"s1"}, ExecutionModel: "fastpath"}, config.LoggingConfig{}, nil); err != nil {
		t.Fatalf("add initial task: %v", err)
	}

	cfg := config.Config{
		Version:   2,
		Receivers: map[string]config.ReceiverConfig{"r1": {Type: "udp_gnet", Listen: ":1"}},
		Senders: map[string]config.SenderConfig{
			"s1": {Type: "udp_unicast", LocalPort: 39002, Remote: "127.0.0.1:9"},
		},
		Pipelines: map[string][]config.StageConfig{"p1": {}},
		Tasks: map[string]config.TaskConfig{
			"t1": {Receivers: []string{"r1"}, Pipelines: []string{"p1"}, Senders: []string{"s1"}, ExecutionModel: "fastpath"},
		},
		Logging: config.LoggingConfig{},
	}

	if err := st.applyBusinessDelta(context.Background(), cfg); err != nil {
		t.Fatalf("apply delta: %v", err)
	}

	if len(st.tasks["t1"].T.Senders) != 1 {
		t.Fatalf("expected 1 sender on task")
	}
	if got := st.tasks["t1"].T.Senders[0].Name(); got == "old-s1" {
		t.Fatalf("expected rebuilt task to avoid stale sender instance, still got %s", got)
	}
	if got := st.senders["s1"].S.Name(); got != st.tasks["t1"].T.Senders[0].Name() {
		t.Fatalf("expected task sender to match runtime sender, task=%s runtime=%s", st.tasks["t1"].T.Senders[0].Name(), got)
	}
}

func TestApplyBusinessDeltaRebuildsTasksForChangedPipelineConfig(t *testing.T) {
	st := NewStore()
	st.senders["s1"] = &SenderState{Name: "s1", Cfg: config.SenderConfig{Type: "udp_unicast", LocalPort: 39003, Remote: "127.0.0.1:9"}, S: &captureSender{name: "s1"}}
	st.pipelines["p1"] = &CompiledPipeline{Name: "p1", P: &pipeline.Pipeline{Name: "old-p1"}}
	st.pipelineCfg = map[string][]config.StageConfig{"p1": {}}
	st.subs["r1"] = map[string]struct{}{}

	if err := st.addTask("t1", config.TaskConfig{Receivers: []string{"r1"}, Pipelines: []string{"p1"}, Senders: []string{"s1"}, ExecutionModel: "fastpath"}, config.LoggingConfig{}, nil); err != nil {
		t.Fatalf("add initial task: %v", err)
	}
	initialPtr := fmt.Sprintf("%p", st.tasks["t1"].T.Pipelines[0])

	cfg := config.Config{
		Version:   2,
		Receivers: map[string]config.ReceiverConfig{"r1": {Type: "udp_gnet", Listen: ":1"}},
		Senders:   map[string]config.SenderConfig{"s1": {Type: "udp_unicast", LocalPort: 39003, Remote: "127.0.0.1:9"}},
		Pipelines: map[string][]config.StageConfig{"p1": {{Type: "clear_file_meta"}}},
		Tasks: map[string]config.TaskConfig{
			"t1": {Receivers: []string{"r1"}, Pipelines: []string{"p1"}, Senders: []string{"s1"}, ExecutionModel: "fastpath"},
		},
		Logging: config.LoggingConfig{},
	}

	if err := st.applyBusinessDelta(context.Background(), cfg); err != nil {
		t.Fatalf("apply delta: %v", err)
	}

	updatedPtr := fmt.Sprintf("%p", st.tasks["t1"].T.Pipelines[0])
	if updatedPtr == initialPtr {
		t.Fatalf("expected task to be rebuilt with new pipeline pointer")
	}
}
