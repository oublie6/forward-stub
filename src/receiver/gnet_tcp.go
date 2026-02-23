package receiver

import (
	"context"
	"sync"

	"forword-stub/src/logx"
	"forword-stub/src/packet"

	"github.com/panjf2000/gnet/v2"
	"github.com/panjf2000/gnet/v2/pkg/logging"
)

type GnetTCP struct {
	name         string
	listen       string
	multicore    bool
	framer       Framer
	gnetLogLevel logging.Level

	onPacket func(*packet.Packet)

	stopMu sync.Mutex
	stopFn func(context.Context) error
}

func NewGnetTCP(name, listen string, multicore bool, framer Framer, gnetLogLevel string) *GnetTCP {
	return &GnetTCP{
		name:         name,
		listen:       listen,
		multicore:    multicore,
		framer:       framer,
		gnetLogLevel: logx.ParseGnetLogLevel(gnetLogLevel),
	}
}

func (r *GnetTCP) Name() string { return r.name }
func (r *GnetTCP) Key() string  { return "tcp_gnet|" + r.listen }

func (r *GnetTCP) Start(ctx context.Context, onPacket func(*packet.Packet)) error {
	r.onPacket = onPacket
	return gnet.Run(
		&tcpHandler{recv: r},
		r.listen,
		gnet.WithMulticore(r.multicore),
		gnet.WithReusePort(true),
		gnet.WithReuseAddr(true),
		gnet.WithLogLevel(r.gnetLogLevel),
	)
}

func (r *GnetTCP) Stop(ctx context.Context) error {
	r.stopMu.Lock()
	fn := r.stopFn
	r.stopMu.Unlock()
	if fn != nil {
		return fn(ctx)
	}
	return nil
}

type connState struct {
	buf    []byte
	remote string
	local  string
}

type tcpHandler struct {
	gnet.BuiltinEventEngine
	recv *GnetTCP
}

func (h *tcpHandler) OnBoot(eng gnet.Engine) (action gnet.Action) {
	h.recv.stopMu.Lock()
	h.recv.stopFn = eng.Stop
	h.recv.stopMu.Unlock()
	return gnet.None
}

func (h *tcpHandler) OnOpen(c gnet.Conn) (out []byte, action gnet.Action) {
	c.SetContext(&connState{
		buf:    make([]byte, 0, 4096),
		remote: c.RemoteAddr().String(),
		local:  c.LocalAddr().String(),
	})
	return nil, gnet.None
}

func (h *tcpHandler) OnTraffic(c gnet.Conn) gnet.Action {
	in, _ := c.Next(-1)
	if len(in) == 0 {
		return gnet.None
	}
	cs := c.Context().(*connState)
	cs.buf = append(cs.buf, in...)

	if h.recv.framer == nil {
		payload, rel := packet.CopyFrom(cs.buf)
		cs.buf = cs.buf[:0]
		h.recv.onPacket(&packet.Packet{
			Payload: payload,
			Meta: packet.Meta{
				Proto:  packet.ProtoTCP,
				Remote: cs.remote,
				Local:  cs.local,
			},
			ReleaseFn: rel,
		})
		return gnet.None
	}

	frames, remain, err := h.recv.framer.Feed(cs.buf)
	if err != nil {
		return gnet.Close
	}
	cs.buf = append(cs.buf[:0], remain...)

	for _, fr := range frames {
		payload, rel := packet.CopyFrom(fr)
		h.recv.onPacket(&packet.Packet{
			Payload: payload,
			Meta: packet.Meta{
				Proto:  packet.ProtoTCP,
				Remote: cs.remote,
				Local:  cs.local,
			},
			ReleaseFn: rel,
		})
	}
	return gnet.None
}
