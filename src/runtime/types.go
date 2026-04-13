package runtime

import (
	"sync/atomic"

	"forward-stub/src/config"
	"forward-stub/src/pipeline"
	"forward-stub/src/receiver"
	"forward-stub/src/selector"
	"forward-stub/src/sender"
	"forward-stub/src/task"
)

type ReceiverState struct {
	// Name 是 receiver 配置名，也是日志和 dispatch 查找的稳定标识。
	Name           string
	Cfg            config.ReceiverConfig
	Recv           receiver.Receiver
	LogPayloadRecv bool
	PayloadLogMax  int
	// SelectorName 保存 receiver 当前绑定的 selector 配置名。
	// reload 时 receiver 配置变化会重建该状态；selector/task_set 变化则只替换 Selector 快照。
	SelectorName string
	// Selector 保存已编译的只读 selector 快照。
	// dispatch 热路径只做 atomic load，不持 Store 锁；写入只发生在 rebuildReceiverSelectors 冷路径。
	Selector atomic.Value // *CompiledSelector
}

type SenderState struct {
	// Name 是 sender 配置名；task 通过配置名引用 sender。
	Name string
	Cfg  config.SenderConfig
	S    sender.Sender
	// Refs 是当前仍被 task 引用的计数，只在 Store 锁下读写。
	// sender 配置变化时，旧实例只有在 Refs 归零后才能安全关闭。
	Refs int
}

type TaskState struct {
	// Name 是 task 配置名；selector 编译结果直接持有 *TaskState。
	Name string
	Cfg  config.TaskConfig
	T    *task.Task
}

type CompiledPipeline struct {
	// Name 是 pipeline 配置名。
	Name string
	// P 是编译后的 stage 链；具体 task 会按当前配置构造 task 作用域实例。
	P *pipeline.Pipeline
}

type StageCacheEntry struct {
	// Sig 是 stage 配置的稳定签名，用于 reload 时复用等价 stage。
	Sig string
	Fn  pipeline.StageFunc
	// TaskRefs/Tasks 跟踪哪些 task 正在引用该 stage cache entry。
	// 引用计数只在 Store 锁下维护，避免 reload 期间误删仍在使用的 stage。
	TaskRefs int
	Tasks    map[string]struct{}
}

type CompiledSelector = selector.Compiled[*TaskState]

func newCompiledSelector(name string, matchKeyCapacity int) *CompiledSelector {
	return selector.NewCompiled[*TaskState](name, matchKeyCapacity)
}

func newDefaultOnlyCompiledSelector(name string, tasks []*TaskState) *CompiledSelector {
	return selector.NewDefaultOnlyCompiled[*TaskState](name, tasks)
}
