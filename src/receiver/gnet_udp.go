package receiver

import (
	"context"
	"sync"

	"forword-stub/src/packet"

	"github.com/panjf2000/gnet/v2"
)

type GnetUDP struct {
	name      string
	listen    string
	multicore bool

	onPacket func(*packet.Packet)

	stopMu sync.Mutex
	stopFn func(context.Context) error
}

func NewGnetUDP(name, listen string, multicore bool) *GnetUDP {
	return &GnetUDP{name: name, listen: listen, multicore: multicore}
}

func (r *GnetUDP) Name() string { return r.name }
func (r *GnetUDP) Key() string  { return "udp_gnet|" + r.listen }

func (r *GnetUDP) Start(ctx context.Context, onPacket func(*packet.Packet)) error {
	r.onPacket = onPacket
	return gnet.Run(&udpHandler{recv: r}, r.listen, gnet.WithMulticore(r.multicore), gnet.WithReusePort(true), gnet.WithReuseAddr(true))
}

func (r *GnetUDP) Stop(ctx context.Context) error {
	r.stopMu.Lock()
	fn := r.stopFn
	r.stopMu.Unlock()
	if fn != nil {
		return fn(ctx)
	}
	return nil
}

type udpHandler struct {
	gnet.BuiltinEventEngine
	recv *GnetUDP
}

func (h *udpHandler) OnBoot(eng gnet.Engine) (action gnet.Action) {
	h.recv.stopMu.Lock()
	h.recv.stopFn = eng.Stop
	h.recv.stopMu.Unlock()
	return gnet.None
}

func (h *udpHandler) OnTraffic(c gnet.Conn) gnet.Action {
	in, _ := c.Next(-1)
	if len(in) == 0 {
		return gnet.None
	}
	payload, rel := packet.CopyFrom(in)
	h.recv.onPacket(&packet.Packet{
		Payload: payload,
		Meta: packet.Meta{
			Proto:  packet.ProtoUDP,
			Remote: c.RemoteAddr().String(),
			Local:  c.LocalAddr().String(),
		},
		ReleaseFn: rel,
	})
	return gnet.None
}
