// pool.go 封装 payload 缓冲池，减少高频内存分配。
package packet

import (
	"sync"
	"sync/atomic"
)

var payloadPool sync.Pool

var payloadPoolGets atomic.Int64
var payloadPoolPuts atomic.Int64
var payloadPoolInUseBuffers atomic.Int64
var payloadPoolInUseBytes atomic.Int64
var payloadPoolCachedBuffers atomic.Int64
var payloadPoolCachedBytes atomic.Int64
var payloadPoolMaxCachedBytes atomic.Int64

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

// SetPayloadPoolMaxCachedBytes 设置 payload 池可缓存的最大字节数。
// <=0 表示不限制（默认）。
func SetPayloadPoolMaxCachedBytes(v int64) {
	payloadPoolMaxCachedBytes.Store(v)
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
	b, _ := payloadPool.Get().([]byte)
	if cap(b) > 0 {
		payloadPoolCachedBuffers.Add(-1)
		payloadPoolCachedBytes.Add(-int64(cap(b)))
	}
	b = append(b[:0], in...)
	n := int64(len(b))
	payloadPoolGets.Add(1)
	payloadPoolInUseBuffers.Add(1)
	payloadPoolInUseBytes.Add(n)
	return b, func() {
		payloadPoolPuts.Add(1)
		payloadPoolInUseBuffers.Add(-1)
		payloadPoolInUseBytes.Add(-n)

		capN := int64(cap(b))
		maxCached := payloadPoolMaxCachedBytes.Load()
		if maxCached > 0 && (payloadPoolCachedBytes.Load()+capN > maxCached) {
			return
		}
		payloadPoolCachedBuffers.Add(1)
		payloadPoolCachedBytes.Add(capN)
		payloadPool.Put(b[:0])
	}
}
