package packet

import (
	"sync"
	"sync/atomic"
)

var (
	payloadPool sync.Pool

	payloadPoolMaxCachedBytes atomic.Int64
	payloadPoolCachedBytes    atomic.Int64
)

func SetPayloadPoolMaxCachedBytes(maxCachedBytes int64) {
	if maxCachedBytes < 0 {
		maxCachedBytes = 0
	}
	payloadPoolMaxCachedBytes.Store(maxCachedBytes)
}

func CopyFrom(in []byte) (out []byte, release func()) {
	bufPtr := payloadPool.Get()
	var buf []byte
	if bufPtr != nil {
		buf = *(bufPtr.(*[]byte))
		payloadPoolCachedBytes.Add(-int64(cap(buf)))
	} else {
		buf = make([]byte, 0, len(in))
	}
	if cap(buf) < len(in) {
		buf = make([]byte, len(in))
	} else {
		buf = buf[:len(in)]
	}
	copy(buf, in)
	return buf, func() {
		releasePayloadBuffer(buf)
	}
}

func releasePayloadBuffer(buf []byte) {
	if buf == nil {
		return
	}
	size := int64(cap(buf))
	maxCached := payloadPoolMaxCachedBytes.Load()
	if maxCached > 0 {
		for {
			cached := payloadPoolCachedBytes.Load()
			if cached+size > maxCached {
				return
			}
			if payloadPoolCachedBytes.CompareAndSwap(cached, cached+size) {
				break
			}
		}
	}
	buf = buf[:0]
	bufPtr := &buf
	payloadPool.Put(bufPtr)
}
