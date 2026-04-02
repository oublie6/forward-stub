package pipeline

import "forward-stub/src/packet"

// RouteSenderByOffsetBytes 根据固定偏移字段选择目标 sender。
func RouteSenderByOffsetBytes(offset int, keyLen int, routes map[string]string, defaultSender string) StageFunc {
	if offset < 0 || keyLen <= 0 {
		return mapStage(func(*packet.Packet) bool { return false })
	}
	end := offset + keyLen
	if end < offset {
		return mapStage(func(*packet.Packet) bool { return false })
	}
	return mapStage(func(p *packet.Packet) bool {
		if end > len(p.Payload) {
			return false
		}
		if sn, ok := routes[string(p.Payload[offset:end])]; ok {
			p.Meta.RouteSender = sn
			return true
		}
		if defaultSender != "" {
			p.Meta.RouteSender = defaultSender
			return true
		}
		return false
	})
}
