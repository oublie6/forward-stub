// pipeline.go 定义处理链抽象与按顺序执行的核心逻辑。
package pipeline

import "forward-stub/src/packet"

type StageFunc func([]*packet.Packet) []*packet.Packet

type Pipeline struct {
	Name   string
	Stages []StageFunc
}

// Process 负责该函数对应的核心逻辑，详见实现细节。
func (pl *Pipeline) Process(p *packet.Packet) []*packet.Packet {
	packets := []*packet.Packet{p}
	for _, st := range pl.Stages {
		packets = st(packets)
		if len(packets) == 0 {
			return nil
		}
	}
	return packets
}
