package pipeline

import "forward-stub/src/packet"

func mapStage(one func(*packet.Packet) bool) StageFunc {
	return func(in []*packet.Packet) []*packet.Packet {
		if len(in) == 0 {
			return nil
		}
		// 热路径通常是所有 packet 通过；复用输入 slice，只有发生过滤时才分配输出。
		for i, p := range in {
			if !one(p) {
				out := make([]*packet.Packet, 0, len(in)-1)
				out = append(out, in[:i]...)
				for _, rest := range in[i+1:] {
					if one(rest) {
						out = append(out, rest)
					}
				}
				return out
			}
		}
		return in
	}
}
