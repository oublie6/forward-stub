package pipeline

import "forward-stub/src/packet"

func mapStage(one func(*packet.Packet) bool) StageFunc {
	return func(in []*packet.Packet) []*packet.Packet {
		if len(in) == 0 {
			return nil
		}
		out := make([]*packet.Packet, 0, len(in))
		for _, p := range in {
			if one(p) {
				out = append(out, p)
			}
		}
		return out
	}
}
