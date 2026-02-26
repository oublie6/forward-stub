// pool.go 封装 payload 缓冲池，减少高频内存分配。
package packet

import (
	"sync"

	"github.com/valyala/bytebufferpool"
)

const (
	defaultPayloadPoolSize      = 1024
	defaultPayloadMaxReuseBytes = 0 // 0 表示不限制单 buffer 复用上限（兼容旧行为）
)

var payloadBufferPool = &bufferPool{}

type bufferPool struct {
	mu            sync.RWMutex
	ch            chan *bytebufferpool.ByteBuffer
	maxReuseBytes int
}

func newBufferPool(size, maxReuseBytes int) *bufferPool {
	if size <= 0 {
		size = defaultPayloadPoolSize
	}
	if maxReuseBytes < 0 {
		maxReuseBytes = defaultPayloadMaxReuseBytes
	}
	return &bufferPool{
		ch:            make(chan *bytebufferpool.ByteBuffer, size),
		maxReuseBytes: maxReuseBytes,
	}
}

// ConfigurePayloadPool 支持配置 payload 内存池大小与可复用上限。
func ConfigurePayloadPool(size, maxReuseBytes int) {
	payloadBufferPool.mu.Lock()
	defer payloadBufferPool.mu.Unlock()
	cfg := newBufferPool(size, maxReuseBytes)
	payloadBufferPool.ch = cfg.ch
	payloadBufferPool.maxReuseBytes = cfg.maxReuseBytes
}

func init() {
	ConfigurePayloadPool(defaultPayloadPoolSize, defaultPayloadMaxReuseBytes)
}

func (p *bufferPool) get() *bytebufferpool.ByteBuffer {
	p.mu.RLock()
	ch := p.ch
	p.mu.RUnlock()
	select {
	case bb := <-ch:
		return bb
	default:
		return &bytebufferpool.ByteBuffer{}
	}
}

func (p *bufferPool) put(bb *bytebufferpool.ByteBuffer) {
	p.mu.RLock()
	ch := p.ch
	maxReuseBytes := p.maxReuseBytes
	p.mu.RUnlock()
	if maxReuseBytes > 0 && cap(bb.B) > maxReuseBytes {
		return
	}
	bb.B = bb.B[:0]
	select {
	case ch <- bb:
	default:
	}
}

// CopyFrom 负责该函数对应的核心逻辑，详见实现细节。
func CopyFrom(in []byte) (out []byte, release func()) {
	bb := payloadBufferPool.get()
	bb.B = append(bb.B[:0], in...)
	return bb.B, func() {
		payloadBufferPool.put(bb)
	}
}
