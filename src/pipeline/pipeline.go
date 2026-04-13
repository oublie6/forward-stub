// pipeline.go 定义处理链抽象与按顺序执行的核心逻辑。
package pipeline

import "forward-stub/src/packet"

// StageFunc 是 pipeline 内部处理一组 packet 的最小单元。
//
// 约束：
//   - 返回空切片表示该 pipeline 终止；
//   - stage 可以原地改写 packet，但若生成新 packet，必须让调用方能负责 Release；
//   - 常规热路径应尽量复用输入切片，避免 per-packet 分配。
type StageFunc func([]*packet.Packet) []*packet.Packet

// Pipeline 是按顺序执行的 stage 链。
// 它不参与 selector 主路由，只在 task 命中后处理 packet。
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
