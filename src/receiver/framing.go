package receiver

import "encoding/binary"

type Framer interface {
	Feed(in []byte) (frames [][]byte, remain []byte, err error)
}

type U16BEFramer struct{}

func (f U16BEFramer) Feed(in []byte) ([][]byte, []byte, error) {
	var frames [][]byte
	buf := in
	for {
		if len(buf) < 2 {
			return frames, buf, nil
		}
		n := int(binary.BigEndian.Uint16(buf[:2]))
		if len(buf) < 2+n {
			return frames, buf, nil
		}
		frames = append(frames, buf[2:2+n])
		buf = buf[2+n:]
	}
}
