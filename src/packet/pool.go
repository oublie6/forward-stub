// pool.go 封装 payload 缓冲池，减少高频内存分配。
package packet

import (
	"sync/atomic"

	"github.com/valyala/bytebufferpool"
)

var PayloadPool bytebufferpool.Pool

var payloadPoolGets atomic.Int64
var payloadPoolPuts atomic.Int64
var payloadPoolInUseBuffers atomic.Int64
var payloadPoolInUseBytes atomic.Int64

type PayloadPoolStats struct {
	Gets         int64
	Puts         int64
	InUseBuffers int64
	InUseBytes   int64
}

// SnapshotPayloadPoolStats 返回当前 payload 内存池统计快照。
func SnapshotPayloadPoolStats() PayloadPoolStats {
	return PayloadPoolStats{
		Gets:         payloadPoolGets.Load(),
		Puts:         payloadPoolPuts.Load(),
		InUseBuffers: payloadPoolInUseBuffers.Load(),
		InUseBytes:   payloadPoolInUseBytes.Load(),
	}
}

// CopyFrom 负责该函数对应的核心逻辑，详见实现细节。
func CopyFrom(in []byte) (out []byte, release func()) {
	bb := PayloadPool.Get()
	bb.B = append(bb.B[:0], in...)
	n := int64(len(bb.B))
	payloadPoolGets.Add(1)
	payloadPoolInUseBuffers.Add(1)
	payloadPoolInUseBytes.Add(n)
	return bb.B, func() {
		payloadPoolPuts.Add(1)
		payloadPoolInUseBuffers.Add(-1)
		payloadPoolInUseBytes.Add(-n)
		bb.B = bb.B[:0]
		PayloadPool.Put(bb)
	}
}
