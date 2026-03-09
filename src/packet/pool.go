// pool.go 封装 payload 缓冲池，减少高频内存分配。
package packet

import "github.com/valyala/bytebufferpool"

var payloadPool bytebufferpool.Pool

// SetPayloadPoolMaxCachedBytes 设置 payload 池可缓存的最大字节数。
// 当前使用底层内存池自适应策略，该设置保留为兼容入口。
func SetPayloadPoolMaxCachedBytes(_ int64) {}

// CopyFrom 负责该函数对应的核心逻辑，详见实现细节。
func CopyFrom(in []byte) (out []byte, release func()) {
	bb := payloadPool.Get()
	bb.B = append(bb.B[:0], in...)
	return bb.B, func() {
		bb.Reset()
		payloadPool.Put(bb)
	}
}
