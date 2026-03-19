// Package runtime 负责维护转发运行时对象及其测试辅助逻辑。
package runtime

import (
	"context"
	"testing"

	"forward-stub/src/config"
	"forward-stub/src/packet"
	"forward-stub/src/sender"
	"forward-stub/src/task"
)

// newFastTask 创建一个 fastpath 测试任务，减少各个路由测试中的重复搭建代码。
func newFastTask(t *testing.T, name string, s sender.Sender) *task.Task {
	t.Helper()
	tk := &task.Task{Name: name, FastPath: true, Senders: []sender.Sender{s}}
	if err := tk.Start(); err != nil {
		t.Fatalf("start task %s: %v", name, err)
	}
	return tk
}

// TestSelectorCompileExpandsTaskSetReuse 验证 selector 编译时会把 task_set 直接展开为任务切片。
func TestSelectorCompileExpandsTaskSetReuse(t *testing.T) {
	st := NewStore()
	s1 := &captureSender{testNamedSender: testNamedSender{name: "s1"}}
	s2 := &captureSender{testNamedSender: testNamedSender{name: "s2"}}
	t1 := newFastTask(t, "t1", s1)
	t2 := newFastTask(t, "t2", s2)
	defer t1.StopGraceful()
	defer t2.StopGraceful()

	st.tasks["t1"] = &TaskState{Name: "t1", T: t1}
	st.tasks["t2"] = &TaskState{Name: "t2", T: t2}
	st.selectors["sel1"] = config.SelectorConfig{
		Matches: map[string]string{
			"udp|src_addr=1.1.1.1:9000": "ts_shared",
			"udp|src_addr=2.2.2.2:9000": "ts_shared",
		},
	}
	st.taskSets["ts_shared"] = []string{"t1", "t2"}
	rs := &ReceiverState{Name: "r1", SelectorName: "sel1"}
	st.receivers["r1"] = rs

	if err := st.rebuildReceiverSelectors(); err != nil {
		t.Fatalf("rebuild selectors: %v", err)
	}

	selector := rs.Selector.Load().(*CompiledSelector)
	gotA := selector.Match("udp|src_addr=1.1.1.1:9000")
	gotB := selector.Match("udp|src_addr=2.2.2.2:9000")
	if len(gotA) != 2 || len(gotB) != 2 {
		t.Fatalf("expected both keys expanded to 2 tasks, gotA=%d gotB=%d", len(gotA), len(gotB))
	}
	if gotA[0].Name != "t1" || gotA[1].Name != "t2" || gotB[0].Name != "t1" || gotB[1].Name != "t2" {
		t.Fatalf("unexpected task expansion: gotA=%v gotB=%v", []string{gotA[0].Name, gotA[1].Name}, []string{gotB[0].Name, gotB[1].Name})
	}
}

// TestDispatchUsesMatchKeyAndDefaultTasks 验证 dispatch 会优先使用 MatchKey 命中规则，并在未命中时走默认任务集。
func TestDispatchUsesMatchKeyAndDefaultTasks(t *testing.T) {
	ctx := context.Background()
	sKafka := &captureSender{testNamedSender: testNamedSender{name: "kafka"}}
	sDefault := &captureSender{testNamedSender: testNamedSender{name: "default"}}
	tKafka := newFastTask(t, "kafka-task", sKafka)
	tDefault := newFastTask(t, "default-task", sDefault)
	defer tKafka.StopGraceful()
	defer tDefault.StopGraceful()

	rs := &ReceiverState{Name: "rx", LogPayloadRecv: false}
	selector := newCompiledSelector("sel1", 1)
	selector.TasksByKey["kafka|topic=orders|partition=3"] = []*TaskState{{Name: "kafka-task", T: tKafka}}
	selector.DefaultTasks = []*TaskState{{Name: "default-task", T: tDefault}}
	rs.Selector.Store(selector)

	dispatchToSelector(ctx, rs, &packet.Packet{
		Envelope: packet.Envelope{
			Payload: []byte("kafka-hit"),
			Meta: packet.Meta{
				Remote:   "127.0.0.1:9092",
				MatchKey: "kafka|topic=orders|partition=3",
			},
		},
	})
	if got := string(sKafka.Last()); got != "kafka-hit" {
		t.Fatalf("expected kafka task receive payload, got=%q", got)
	}

	dispatchToSelector(ctx, rs, &packet.Packet{
		Envelope: packet.Envelope{
			Payload: []byte("default-hit"),
			Meta: packet.Meta{
				Remote:   "orders",
				MatchKey: "sftp|remote_dir=/input|file_name=a.txt",
			},
		},
	})
	if got := string(sDefault.Last()); got != "default-hit" {
		t.Fatalf("expected default task receive payload, got=%q", got)
	}
}
