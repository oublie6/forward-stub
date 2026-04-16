package pipeline

import (
	"encoding/hex"

	"forward-stub/src/packet"
)

// SetKafkaRecordKeyFromOffsetBytes 从 payload 固定区间提取 Kafka record key。
// 编译阶段固定 offset/length/encoding；热路径只做边界检查和一次字符串编码。
func SetKafkaRecordKeyFromOffsetBytes(offset int, length int, encoding string) StageFunc {
	if offset < 0 || length <= 0 {
		return mapStage(func(*packet.Packet) bool { return false })
	}
	end := offset + length
	if end < offset {
		return mapStage(func(*packet.Packet) bool { return false })
	}

	switch encoding {
	case "text":
		return mapStage(func(p *packet.Packet) bool {
			if end > len(p.Payload) {
				return false
			}
			p.Meta.KafkaRecordKey = string(p.Payload[offset:end])
			return true
		})
	case "hex":
		return mapStage(func(p *packet.Packet) bool {
			if end > len(p.Payload) {
				return false
			}
			p.Meta.KafkaRecordKey = hex.EncodeToString(p.Payload[offset:end])
			return true
		})
	default:
		return mapStage(func(*packet.Packet) bool { return false })
	}
}
