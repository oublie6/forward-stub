// stages_offset.go 提供按偏移匹配字节等基础 stage 实现。
package pipeline

import (
	"bytes"

	"forword-stub/src/packet"
)

func MatchOffsetBytes(offset int, want []byte, setFlag uint32) StageFunc {
	return func(p *packet.Packet) bool {
		if offset < 0 || offset+len(want) > len(p.Payload) {
			return false
		}
		if !bytes.Equal(p.Payload[offset:offset+len(want)], want) {
			return false
		}
		if setFlag != 0 {
			p.Meta.Flags |= setFlag
		}
		return true
	}
}

func ReplaceOffsetBytes(offset int, with []byte, setFlag uint32) StageFunc {
	return func(p *packet.Packet) bool {
		if offset < 0 || offset+len(with) > len(p.Payload) {
			return false
		}
		copy(p.Payload[offset:offset+len(with)], with)
		if setFlag != 0 {
			p.Meta.Flags |= setFlag
		}
		return true
	}
}

func DropIfFlag(flag uint32) StageFunc {
	return func(p *packet.Packet) bool {
		return (p.Meta.Flags & flag) == 0
	}
}
