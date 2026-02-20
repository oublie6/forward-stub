package runtime

import (
	"forword-stub/src/config"
	"forword-stub/src/pipeline"
	"forword-stub/src/receiver"
	"forword-stub/src/sender"
	"forword-stub/src/task"
)

type ReceiverState struct {
	Name    string
	Cfg     config.ReceiverConfig
	Recv    receiver.Receiver
	Running bool
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
