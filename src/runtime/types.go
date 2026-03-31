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
	Name           string
	Cfg            config.ReceiverConfig
	Recv           receiver.Receiver
	LogPayloadRecv bool
	PayloadLogMax  int
	SelectorName   string
	Selector       atomic.Value // *CompiledSelector
}

type SenderState struct {
	Name string
	Cfg  config.SenderConfig
	S    sender.Sender
	Refs int
}

type TaskState struct {
	Name string
	Cfg  config.TaskConfig
	T    *task.Task
}

type CompiledPipeline struct {
	Name string
	P    *pipeline.Pipeline
}

type StageCacheEntry struct {
	Sig      string
	Fn       pipeline.StageFunc
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
