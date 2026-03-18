// pipeline.go 定义处理链抽象与按顺序执行的核心逻辑。
package pipeline

import "forward-stub/src/packet"

// StageFunc describes pipeline-level state used by the forwarding architecture.
type StageFunc func(*packet.Packet) bool

// Pipeline describes pipeline-level state used by the forwarding architecture.
type Pipeline struct {
	Name   string
	Stages []StageFunc
}

// Process 负责该函数对应的核心逻辑，详见实现细节。
func (pl *Pipeline) Process(p *packet.Packet) bool {
	for _, st := range pl.Stages {
		if !st(p) {
			return false
		}
	}
	return true
}
