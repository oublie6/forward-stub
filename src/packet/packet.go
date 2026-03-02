// packet.go 定义统一报文结构与复制、释放等方法。
package packet

type Proto uint8

type PayloadKind uint8

const (
	ProtoUDP   Proto = 1
	ProtoTCP   Proto = 2
	ProtoKafka Proto = 3
	ProtoSFTP  Proto = 4
)

const (
	PayloadKindStream    PayloadKind = 1
	PayloadKindFileChunk PayloadKind = 2
)

type Meta struct {
	Proto  Proto
	Flags  uint32
	Remote string
	Local  string

	TransferID string
	FileName   string
	FilePath   string
	Offset     int64
	TotalSize  int64
	Checksum   string
	EOF        bool
}

type Envelope struct {
	Kind PayloadKind
	Meta Meta

	Payload []byte
}

type Packet struct {
	Envelope
	ReleaseFn func()
}

// Release 负责该函数对应的核心逻辑，详见实现细节。
func (p *Packet) Release() {
	if p.ReleaseFn != nil {
		p.ReleaseFn()
		p.ReleaseFn = nil
	}
}

// Clone 负责该函数对应的核心逻辑，详见实现细节。
func (p *Packet) Clone() *Packet {
	out, rel := CopyFrom(p.Payload)
	cp := p.Envelope
	cp.Payload = out
	return &Packet{Envelope: cp, ReleaseFn: rel}
}
