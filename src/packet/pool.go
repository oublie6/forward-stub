package packet

import "github.com/valyala/bytebufferpool"

var payloadPool bytebufferpool.Pool

func CopyFrom(in []byte) (out []byte, release func()) {
	bb := payloadPool.Get()
	bb.B = append(bb.B[:0], in...)
	return bb.B, func() {
		bb.Reset()
		payloadPool.Put(bb)
	}
}
