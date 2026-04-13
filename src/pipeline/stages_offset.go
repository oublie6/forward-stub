// stages_offset.go 提供按偏移匹配字节等基础 stage 实现。
package pipeline

import (
	"bytes"
	"encoding/binary"

	"forward-stub/src/packet"
)

// MatchOffsetBytes 在 payload 固定偏移处做字节匹配。
// 匹配值会在编译阶段预处理，热路径只做边界检查和定长比较。
func MatchOffsetBytes(offset int, want []byte) StageFunc {
	if offset < 0 {
		return mapStage(func(*packet.Packet) bool { return false })
	}
	wantLen := len(want)
	end := offset + wantLen
	if end < offset {
		return mapStage(func(*packet.Packet) bool { return false })
	}

	matcher := buildOffsetMatcher(want)
	return mapStage(func(p *packet.Packet) bool {
		if end > len(p.Payload) {
			return false
		}
		return matcher(p.Payload[offset:end])
	})
}

func buildOffsetMatcher(want []byte) func([]byte) bool {
	switch len(want) {
	case 0:
		return func([]byte) bool { return true }
	case 1:
		w0 := want[0]
		return func(got []byte) bool { return got[0] == w0 }
	case 2:
		w := binary.LittleEndian.Uint16(want)
		return func(got []byte) bool { return binary.LittleEndian.Uint16(got) == w }
	case 4:
		w := binary.LittleEndian.Uint32(want)
		return func(got []byte) bool { return binary.LittleEndian.Uint32(got) == w }
	case 8:
		w := binary.LittleEndian.Uint64(want)
		return func(got []byte) bool { return binary.LittleEndian.Uint64(got) == w }
	default:
		return func(got []byte) bool { return bytes.Equal(got, want) }
	}
}

// ReplaceOffsetBytes 在 payload 固定偏移处原地覆盖字节。
// 该 stage 不扩容、不分配；越界时返回 false 终止当前 pipeline。
func ReplaceOffsetBytes(offset int, with []byte) StageFunc {
	if offset < 0 {
		return mapStage(func(*packet.Packet) bool { return false })
	}
	withLen := len(with)
	end := offset + withLen
	if end < offset {
		return mapStage(func(*packet.Packet) bool { return false })
	}

	return mapStage(func(p *packet.Packet) bool {
		if end > len(p.Payload) {
			return false
		}
		copy(p.Payload[offset:end], with)
		return true
	})
}
