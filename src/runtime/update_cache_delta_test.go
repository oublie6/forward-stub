// Package runtime 负责维护转发运行时对象及其测试辅助逻辑。
package runtime

import (
	"context"
	"reflect"
	"testing"

	"forward-stub/src/config"
	"forward-stub/src/pipeline"
)

// TestExpandTaskDeltaForSenderChangesRebuildsImpactedUnchangedTasks 验证 sender 变化会触发依赖它的未改动任务重建。
func TestExpandTaskDeltaForSenderChangesRebuildsImpactedUnchangedTasks(t *testing.T) {
	oldTasks := map[string]config.TaskConfig{
		"t1": {Senders: []string{"s1", "s2"}},
		"t2": {Senders: []string{"s3"}},
	}
	newTasks := map[string]config.TaskConfig{
		"t1": {Senders: []string{"s1", "s2"}},
		"t2": {Senders: []string{"s3"}},
	}

	added, removed := expandTaskDeltaForSenderChanges(
		oldTasks,
		newTasks,
		nil,
		nil,
		[]string{"s1"},
		nil,
	)

	if !reflect.DeepEqual(added, []string{"t1"}) {
		t.Fatalf("unexpected added tasks: %v", added)
	}
	if !reflect.DeepEqual(removed, []string{"t1"}) {
		t.Fatalf("unexpected removed tasks: %v", removed)
	}
}

// TestExpandTaskDeltaForSenderChangesKeepsExistingTaskDeltaAndSorts 验证 sender 影响范围会并入既有 task 增删集合并保持有序。
func TestExpandTaskDeltaForSenderChangesKeepsExistingTaskDeltaAndSorts(t *testing.T) {
	oldTasks := map[string]config.TaskConfig{
		"ta": {Senders: []string{"sx"}},
		"tb": {Senders: []string{"sy"}},
		"tc": {Senders: []string{"sz"}},
	}
	newTasks := map[string]config.TaskConfig{
		"ta": {Senders: []string{"sx"}},
		"tb": {Senders: []string{"sy"}},
		"tc": {Senders: []string{"sz"}},
	}

	added, removed := expandTaskDeltaForSenderChanges(
		oldTasks,
		newTasks,
		[]string{"tc"},
		[]string{"tc"},
		[]string{"sy", "sx"},
		nil,
	)

	if !reflect.DeepEqual(added, []string{"ta", "tb", "tc"}) {
		t.Fatalf("unexpected added tasks: %v", added)
	}
	if !reflect.DeepEqual(removed, []string{"ta", "tb", "tc"}) {
		t.Fatalf("unexpected removed tasks: %v", removed)
	}
}

// TestExpandTaskDeltaForPipelineChangesRebuildsImpactedUnchangedTasks 验证 pipeline 变化会触发依赖它的未改动任务重建。
func TestExpandTaskDeltaForPipelineChangesRebuildsImpactedUnchangedTasks(t *testing.T) {
	oldTasks := map[string]config.TaskConfig{
		"t1": {Pipelines: []string{"p1", "p2"}},
		"t2": {Pipelines: []string{"p3"}},
	}
	newTasks := map[string]config.TaskConfig{
		"t1": {Pipelines: []string{"p1", "p2"}},
		"t2": {Pipelines: []string{"p3"}},
	}

	added, removed := expandTaskDeltaForPipelineChanges(
		oldTasks,
		newTasks,
		nil,
		nil,
		[]string{"p2"},
		nil,
	)

	if !reflect.DeepEqual(added, []string{"t1"}) {
		t.Fatalf("unexpected added tasks: %v", added)
	}
	if !reflect.DeepEqual(removed, []string{"t1"}) {
		t.Fatalf("unexpected removed tasks: %v", removed)
	}
}

// TestPlanBusinessDeltaCartesianThreeByThreeByThreeByThree 穷举多维增删改组合，验证 task 重建计划不会遗漏或重复。
func TestPlanBusinessDeltaCartesianThreeByThreeByThreeByThree(t *testing.T) {
	type op string
	const (
		opAdd    op = "add"
		opRemove op = "remove"
		opModify op = "modify"
	)

	oldReceivers := map[string]config.ReceiverConfig{"r1": {Type: "udp_gnet", Listen: ":10001", Selector: "sel1"}}
	oldSelectors := map[string]config.SelectorConfig{"sel1": {DefaultTaskSet: "ts1"}}
	oldTaskSets := map[string][]string{"ts1": []string{"t1"}}
	oldSenders := map[string]config.SenderConfig{"s1": {Type: "tcp_gnet", Remote: "127.0.0.1:9001"}}
	oldPipelines := map[string][]config.StageConfig{"p1": {{Type: "trim"}}}
	oldTasks := map[string]config.TaskConfig{
		"t1": {Pipelines: []string{"p1"}, Senders: []string{"s1"}},
	}

	applyReceiverOp := func(base map[string]config.ReceiverConfig, o op) map[string]config.ReceiverConfig {
		next := map[string]config.ReceiverConfig{"r1": base["r1"]}
		switch o {
		case opAdd:
			next["r2"] = config.ReceiverConfig{Type: "udp_gnet", Listen: ":10002", Selector: "sel1"}
		case opRemove:
			delete(next, "r1")
		case opModify:
			next["r1"] = config.ReceiverConfig{Type: "udp_gnet", Listen: ":10003", Selector: "sel1"}
		}
		return next
	}
	applySenderOp := func(base map[string]config.SenderConfig, o op) map[string]config.SenderConfig {
		next := map[string]config.SenderConfig{"s1": base["s1"]}
		switch o {
		case opAdd:
			next["s2"] = config.SenderConfig{Type: "tcp_gnet", Remote: "127.0.0.1:9002"}
		case opRemove:
			delete(next, "s1")
		case opModify:
			next["s1"] = config.SenderConfig{Type: "tcp_gnet", Remote: "127.0.0.1:9003"}
		}
		return next
	}
	applyPipelineOp := func(base map[string][]config.StageConfig, o op) map[string][]config.StageConfig {
		next := map[string][]config.StageConfig{"p1": base["p1"]}
		switch o {
		case opAdd:
			next["p2"] = []config.StageConfig{{Type: "trim"}}
		case opRemove:
			delete(next, "p1")
		case opModify:
			next["p1"] = []config.StageConfig{{Type: "drop"}}
		}
		return next
	}
	applyTaskOp := func(base map[string]config.TaskConfig, o op) map[string]config.TaskConfig {
		next := map[string]config.TaskConfig{"t1": base["t1"]}
		switch o {
		case opAdd:
			next["t2"] = config.TaskConfig{Pipelines: []string{"p1"}, Senders: []string{"s1"}}
		case opRemove:
			delete(next, "t1")
		case opModify:
			tc := next["t1"]
			tc.QueueSize = 2048
			next["t1"] = tc
		}
		return next
	}

	ops := []op{opAdd, opRemove, opModify}
	for _, rop := range ops {
		for _, sop := range ops {
			for _, pop := range ops {
				for _, top := range ops {
					nextCfg := config.Config{
						Receivers: applyReceiverOp(oldReceivers, rop),
						Selectors: oldSelectors,
						TaskSets:  oldTaskSets,
						Senders:   applySenderOp(oldSenders, sop),
						Pipelines: applyPipelineOp(oldPipelines, pop),
						Tasks:     applyTaskOp(oldTasks, top),
					}
					plan := planBusinessDelta(oldReceivers, oldSelectors, oldTaskSets, oldSenders, oldPipelines, oldTasks, nextCfg)

					if hasDup(plan.taskAdded) {
						t.Fatalf("taskAdded has duplicates in scenario r=%s s=%s p=%s t=%s: %v", rop, sop, pop, top, plan.taskAdded)
					}
					if hasDup(plan.taskRemoved) {
						t.Fatalf("taskRemoved has duplicates in scenario r=%s s=%s p=%s t=%s: %v", rop, sop, pop, top, plan.taskRemoved)
					}

					t1ExistsInNew := top != opRemove
					if t1ExistsInNew {
						if sop == opModify || sop == opRemove || pop == opModify || pop == opRemove {
							if !contains(plan.taskAdded, "t1") || !contains(plan.taskRemoved, "t1") {
								t.Fatalf("t1 should be rebuilt in scenario r=%s s=%s p=%s t=%s: added=%v removed=%v", rop, sop, pop, top, plan.taskAdded, plan.taskRemoved)
							}
						}
					}
				}
			}
		}
	}
}

// contains 判断字符串切片中是否存在指定项，供差量计划测试断言使用。
func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

// hasDup 判断字符串切片中是否出现重复项，供差量计划测试校验结果唯一性。
func hasDup(items []string) bool {
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if _, ok := seen[item]; ok {
			return true
		}
		seen[item] = struct{}{}
	}
	return false
}

// TestApplyTaskDeltaAddUpdateRemove 验证任务在增量更新中可以完成新增、重建和删除。
func TestApplyTaskDeltaAddUpdateRemove(t *testing.T) {
	st := NewStore()
	st.senders["s1"] = &SenderState{Name: "s1", Cfg: config.SenderConfig{Type: "tcp_gnet", Remote: "127.0.0.1:12345"}, S: &captureSender{name: "s1"}}
	st.pipelines["p1"] = &CompiledPipeline{Name: "p1", P: &pipeline.Pipeline{Name: "p1"}}
	st.pipelineCfg = map[string][]config.StageConfig{"p1": {}}
	st.selectors["sel1"] = config.SelectorConfig{DefaultTaskSet: "ts1"}
	st.taskSets["ts1"] = []string{"t1"}
	st.receivers["r1"] = &ReceiverState{Name: "r1", SelectorName: "sel1"}

	if err := st.addTask("t1", config.TaskConfig{Pipelines: []string{"p1"}, Senders: []string{"s1"}, ExecutionModel: "fastpath"}, config.LoggingConfig{}, nil); err != nil {
		t.Fatalf("add initial task: %v", err)
	}
	if err := st.rebuildReceiverSelectors(); err != nil {
		t.Fatalf("rebuild selectors: %v", err)
	}

	cfg := config.Config{
		Version:   2,
		Receivers: map[string]config.ReceiverConfig{"r1": {Type: "udp_gnet", Listen: ":1", Selector: "sel1"}},
		Selectors: map[string]config.SelectorConfig{"sel1": {DefaultTaskSet: "ts1"}},
		TaskSets:  map[string][]string{"ts1": []string{"t1", "t2"}},
		Senders:   map[string]config.SenderConfig{"s1": {Type: "tcp_gnet", Remote: "127.0.0.1:12345"}},
		Pipelines: map[string][]config.StageConfig{"p1": {}},
		Tasks: map[string]config.TaskConfig{
			"t1": {Pipelines: []string{"p1"}, Senders: []string{"s1"}, ExecutionModel: "pool", QueueSize: 1024},
			"t2": {Pipelines: []string{"p1"}, Senders: []string{"s1"}, ExecutionModel: "fastpath"},
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
	cfg2.TaskSets = map[string][]string{"ts1": []string{"t2"}}
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

// TestRemoveTaskRefreshDispatchSnapshotImmediately 验证任务删除后 selector 快照会立即反映最新路由结果。
func TestRemoveTaskRefreshDispatchSnapshotImmediately(t *testing.T) {
	st := NewStore()
	st.senders["s1"] = &SenderState{Name: "s1", Cfg: config.SenderConfig{Type: "tcp_gnet", Remote: "127.0.0.1:12345"}, S: &captureSender{name: "s1"}}
	st.pipelines["p1"] = &CompiledPipeline{Name: "p1", P: &pipeline.Pipeline{Name: "p1"}}
	st.pipelineCfg = map[string][]config.StageConfig{"p1": {}}
	st.selectors["sel1"] = config.SelectorConfig{DefaultTaskSet: "ts1"}
	st.taskSets["ts1"] = []string{"t1"}
	st.receivers["r1"] = &ReceiverState{Name: "r1", SelectorName: "sel1"}

	if err := st.addTask("t1", config.TaskConfig{Pipelines: []string{"p1"}, Senders: []string{"s1"}, ExecutionModel: "fastpath"}, config.LoggingConfig{}, nil); err != nil {
		t.Fatalf("add task: %v", err)
	}
	if err := st.rebuildReceiverSelectors(); err != nil {
		t.Fatalf("rebuild selectors: %v", err)
	}
	if got := len(st.getDispatchTasks("r1")); got != 1 {
		t.Fatalf("expected dispatch snapshot has 1 task, got %d", got)
	}

	st.taskSets["ts1"] = nil
	if err := st.rebuildReceiverSelectors(); err != nil {
		t.Fatalf("rebuild selectors after remove: %v", err)
	}
	_ = st.removeTask("t1", false)
	if got := len(st.getDispatchTasks("r1")); got != 0 {
		t.Fatalf("expected dispatch snapshot has 0 task after remove, got %d", got)
	}
}

// TestApplyBusinessDeltaUpdatesPayloadLogOptions 验证增量更新后任务 payload 日志配置会同步刷新。
func TestApplyBusinessDeltaUpdatesPayloadLogOptions(t *testing.T) {
	st := NewStore()
	st.senders["s1"] = &SenderState{Name: "s1", Cfg: config.SenderConfig{Type: "tcp_gnet", Remote: "127.0.0.1:12345"}, S: &captureSender{name: "s1"}}
	st.pipelines["p1"] = &CompiledPipeline{Name: "p1", P: &pipeline.Pipeline{Name: "p1"}}
	st.pipelineCfg = map[string][]config.StageConfig{"p1": {}}
	st.selectors["sel1"] = config.SelectorConfig{DefaultTaskSet: "ts1"}
	st.taskSets["ts1"] = []string{"t1"}
	st.receivers["r1"] = &ReceiverState{Name: "r1", SelectorName: "sel1", Cfg: config.ReceiverConfig{Type: "udp_gnet", Listen: ":1", Selector: "sel1"}}

	if err := st.addTask("t1", config.TaskConfig{Pipelines: []string{"p1"}, Senders: []string{"s1"}, ExecutionModel: "fastpath"}, config.LoggingConfig{PayloadLogMaxBytes: 128}, nil); err != nil {
		t.Fatalf("add initial task: %v", err)
	}
	if err := st.rebuildReceiverSelectors(); err != nil {
		t.Fatalf("rebuild selectors: %v", err)
	}

	cfg := config.Config{
		Version:   2,
		Receivers: map[string]config.ReceiverConfig{"r1": {Type: "udp_gnet", Listen: ":1", Selector: "sel1", LogPayloadRecv: true, PayloadLogMaxBytes: 64}},
		Selectors: map[string]config.SelectorConfig{"sel1": {DefaultTaskSet: "ts1"}},
		TaskSets:  map[string][]string{"ts1": []string{"t1"}},
		Senders:   map[string]config.SenderConfig{"s1": {Type: "tcp_gnet", Remote: "127.0.0.1:12345"}},
		Pipelines: map[string][]config.StageConfig{"p1": {}},
		Tasks: map[string]config.TaskConfig{
			"t1": {Pipelines: []string{"p1"}, Senders: []string{"s1"}, ExecutionModel: "fastpath", LogPayloadSend: true, PayloadLogMaxBytes: 32},
		},
		Logging: config.LoggingConfig{PayloadLogMaxBytes: 128},
	}

	if err := st.applyBusinessDelta(context.Background(), cfg); err != nil {
		t.Fatalf("apply delta: %v", err)
	}
	if !st.receivers["r1"].LogPayloadRecv || st.receivers["r1"].PayloadLogMax != 64 {
		t.Fatalf("receiver payload log options not updated: %+v", st.receivers["r1"])
	}
	if !st.tasks["t1"].T.LogPayloadSend || st.tasks["t1"].T.PayloadLogMax != 32 {
		t.Fatalf("task payload log options not updated: %+v", st.tasks["t1"].T)
	}
}
