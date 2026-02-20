package packet

import "github.com/valyala/bytebufferpool"

var PayloadPool bytebufferpool.Pool

func CopyFrom(in []byte) (out []byte, release func()) {
	bb := PayloadPool.Get()
	bb.B = append(bb.B[:0], in...)
	return bb.B, func() {
		bb.B = bb.B[:0]
		PayloadPool.Put(bb)
	}
}
