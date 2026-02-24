// packet.go 定义统一报文结构与复制、释放等方法。
package packet

type Proto uint8

const (
	ProtoUDP   Proto = 1
	ProtoTCP   Proto = 2
	ProtoKafka Proto = 3
)

type Meta struct {
	Proto  Proto
	Flags  uint32
	Remote string
	Local  string
}

type Packet struct {
	Payload   []byte
	Meta      Meta
	ReleaseFn func()
}

func (p *Packet) Release() {
	if p.ReleaseFn != nil {
		p.ReleaseFn()
		p.ReleaseFn = nil
	}
}

func (p *Packet) Clone() *Packet {
	out, rel := CopyFrom(p.Payload)
	return &Packet{Payload: out, Meta: p.Meta, ReleaseFn: rel}
}
