// types.go 定义运行时内部使用的关键聚合类型。
package runtime

import (
	"sync/atomic"

	"forward-stub/src/config"
	"forward-stub/src/pipeline"
	"forward-stub/src/receiver"
	"forward-stub/src/sender"
	"forward-stub/src/task"
)

// ReceiverState 描述一个 receiver 在运行时缓存中的状态。
// 用于 UpdateCache 对比、复用与停启控制。
type ReceiverState struct {
	// Name 是 receiver 配置名。
	// 用法：作为 runtime 索引键，供 task 订阅关系解析。
	Name string
	// Cfg 是 receiver 生效配置快照。
	// 用法：用于热更新时比较配置差异，判断是否需要重建实例。
	Cfg config.ReceiverConfig
	// Recv 是运行中的 receiver 实例。
	// 用法：runtime 通过该接口执行 Start/Stop 生命周期管理。
	Recv receiver.Receiver
	// LogPayloadRecv 表示是否记录该 receiver 入站 payload。
	LogPayloadRecv bool
	// PayloadLogMax 是该 receiver payload 摘要最大字节数。
	PayloadLogMax int
	// SelectorName 是当前 receiver 绑定的 selector 名称。
	SelectorName string
	// Selector 保存热路径只读 selector 快照。
	Selector atomic.Value // *CompiledSelector
}

// SenderState 描述一个 sender 在运行时缓存中的状态。
// Refs 记录被多少 task 引用，用于安全回收。
type SenderState struct {
	// Name 是 sender 配置名。
	// 用法：供 task.Senders 引用并用于运行时查找。
	Name string
	// Cfg 是 sender 生效配置快照。
	// 用法：与新配置对比后决定复用连接还是重建 sender。
	Cfg config.SenderConfig
	// S 是运行中的 sender 实例。
	// 用法：task 在转发阶段调用其 Send/Close 等能力。
	S sender.Sender
	// Refs 是当前被 task 引用计数。
	// 用法：归零后可安全关闭并回收 sender 资源。
	Refs int
}

// TaskState 描述一个 task 在运行时缓存中的状态。
// T 是已启动的任务执行实例。
type TaskState struct {
	// Name 是任务名（配置 key）。
	// 用法：用于运行时日志、指标与热更新映射。
	Name string
	// Cfg 是任务配置快照。
	// 用法：变更时用于判定 worker 池、绑定关系是否需重建。
	Cfg config.TaskConfig
	// T 是已构建并可能已启动的任务实例。
	// 用法：runtime 通过该指针驱动任务启动、停止与投递。
	T *task.Task
}

// CompiledPipeline 表示已编译好的 pipeline 实例。
// Name 对应配置中的 pipeline key。
type CompiledPipeline struct {
	// Name 是 pipeline 名称（配置 key）。
	// 用法：task.Pipelines 按名称引用已编译实例。
	Name string
	// P 是编译完成的 pipeline 执行对象。
	// 用法：在任务处理路径中串联执行各 stage。
	P *pipeline.Pipeline
}

// StageCacheEntry 描述可复用 stage 的缓存条目。
type StageCacheEntry struct {
	Sig      string
	Fn       pipeline.StageFunc
	TaskRefs int
	Tasks    map[string]struct{}
}

// CompiledSelector 是运行时编译后的极简精确匹配器。
type CompiledSelector struct {
	Name         string
	TasksByKey   map[string][]*TaskState
	DefaultTasks []*TaskState
}

func (s *CompiledSelector) Match(key string) []*TaskState {
	if s == nil {
		return nil
	}
	if tasks, ok := s.TasksByKey[key]; ok {
		return tasks
	}
	return s.DefaultTasks
}
