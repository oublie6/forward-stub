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
var payloadPoolCachedBuffers atomic.Int64
var payloadPoolCachedBytes atomic.Int64

type PayloadPoolStats struct {
	Gets          int64
	Puts          int64
	InUseBuffers  int64
	InUseBytes    int64
	CachedBuffers int64
	CachedBytes   int64
	TotalBuffers  int64
	TotalBytes    int64
}

// SnapshotPayloadPoolStats 返回当前 payload 内存池统计快照。
func SnapshotPayloadPoolStats() PayloadPoolStats {
	inUseBuffers := payloadPoolInUseBuffers.Load()
	inUseBytes := payloadPoolInUseBytes.Load()
	cachedBuffers := payloadPoolCachedBuffers.Load()
	cachedBytes := payloadPoolCachedBytes.Load()
	return PayloadPoolStats{
		Gets:          payloadPoolGets.Load(),
		Puts:          payloadPoolPuts.Load(),
		InUseBuffers:  inUseBuffers,
		InUseBytes:    inUseBytes,
		CachedBuffers: cachedBuffers,
		CachedBytes:   cachedBytes,
		TotalBuffers:  inUseBuffers + cachedBuffers,
		TotalBytes:    inUseBytes + cachedBytes,
	}
}

// CopyFrom 负责该函数对应的核心逻辑，详见实现细节。
func CopyFrom(in []byte) (out []byte, release func()) {
	bb := PayloadPool.Get()
	if cap(bb.B) > 0 {
		payloadPoolCachedBuffers.Add(-1)
		payloadPoolCachedBytes.Add(-int64(cap(bb.B)))
	}
	bb.B = append(bb.B[:0], in...)
	n := int64(len(bb.B))
	payloadPoolGets.Add(1)
	payloadPoolInUseBuffers.Add(1)
	payloadPoolInUseBytes.Add(n)
	return bb.B, func() {
		payloadPoolPuts.Add(1)
		payloadPoolInUseBuffers.Add(-1)
		payloadPoolInUseBytes.Add(-n)
		capN := int64(cap(bb.B))
		payloadPoolCachedBuffers.Add(1)
		payloadPoolCachedBytes.Add(capN)
		bb.B = bb.B[:0]
		PayloadPool.Put(bb)
	}
}
