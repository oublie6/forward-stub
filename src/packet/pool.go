// pool.go 封装 payload 缓冲池，减少高频内存分配。
package packet

import "github.com/valyala/bytebufferpool"

var PayloadPool bytebufferpool.Pool

// CopyFrom 负责该函数对应的核心逻辑，详见实现细节。
func CopyFrom(in []byte) (out []byte, release func()) {
	bb := PayloadPool.Get()
	bb.B = append(bb.B[:0], in...)
	return bb.B, func() {
		bb.B = bb.B[:0]
		PayloadPool.Put(bb)
	}
}
