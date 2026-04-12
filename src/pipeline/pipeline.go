// pipeline.go 定义处理链抽象与按顺序执行的核心逻辑。
package pipeline

import "forward-stub/src/packet"

type StageFunc func([]*packet.Packet) []*packet.Packet

type Pipeline struct {
	Name   string
	Stages []StageFunc
}

// Process 按配置顺序执行所有 stage。
// 任一 stage 返回空切片即表示该 packet 被丢弃；无 stage 时原包原样通过。
func (pl *Pipeline) Process(p *packet.Packet) []*packet.Packet {
	var single [1]*packet.Packet
	single[0] = p
	packets := single[:]
	for _, st := range pl.Stages {
		packets = st(packets)
		if len(packets) == 0 {
			return nil
		}
	}
	return packets
}
