// pool.go 封装 payload 缓冲池，减少高频内存分配。
package packet

import "github.com/valyala/bytebufferpool"

var payloadPool bytebufferpool.Pool

// SetPayloadPoolMaxCachedBytes 设置 payload 池可缓存的最大字节数。
// 当前使用底层内存池自适应策略，该设置保留为兼容入口。
func SetPayloadPoolMaxCachedBytes(_ int64) {}

// CopyFrom 从 payload 池中复制一份独立字节切片，并返回配套的 release 函数。
//
// 用法：dispatch clone、receiver 入站复制等路径可复用它来降低短生命周期 payload 的分配成本。
func CopyFrom(in []byte) (out []byte, release func()) {
	bb := payloadPool.Get()
	bb.B = append(bb.B[:0], in...)
	return bb.B, func() {
		bb.Reset()
		payloadPool.Put(bb)
	}
}
