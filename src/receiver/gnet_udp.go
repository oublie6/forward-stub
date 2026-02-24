package receiver

import (
	"context"
	"sync"

	"forword-stub/src/logx"
	"forword-stub/src/packet"

	"github.com/panjf2000/gnet/v2"
	"github.com/panjf2000/gnet/v2/pkg/logging"
	"go.uber.org/zap/zapcore"
)

type GnetUDP struct {
	name         string
	listen       string
	multicore    bool
	gnetLogLevel logging.Level

	onPacket func(*packet.Packet)

	stopMu sync.Mutex
	stopFn func(context.Context) error
	stats  *logx.TrafficCounter
}

func NewGnetUDP(name, listen string, multicore bool, gnetLogLevel string) *GnetUDP {
	return &GnetUDP{name: name, listen: listen, multicore: multicore, gnetLogLevel: logx.ParseGnetLogLevel(gnetLogLevel)}
}

func (r *GnetUDP) Name() string { return r.name }
func (r *GnetUDP) Key() string  { return "udp_gnet|" + r.listen }

func (r *GnetUDP) Start(ctx context.Context, onPacket func(*packet.Packet)) error {
	r.onPacket = onPacket
	if logx.Enabled(zapcore.InfoLevel) {
		r.stats = logx.AcquireTrafficCounter(
			"receiver traffic stats",
			"receiver", r.Name(),
			"receiver_key", r.Key(),
			"proto", "udp",
		)
	}
	defer func() {
		if r.stats != nil {
			r.stats.Close()
			r.stats = nil
		}
	}()
	return gnet.Run(
		&udpHandler{recv: r},
		r.listen,
		gnet.WithMulticore(r.multicore),
		gnet.WithReusePort(true),
		gnet.WithReuseAddr(true),
		gnet.WithLogLevel(r.gnetLogLevel),
	)
}

func (r *GnetUDP) Stop(ctx context.Context) error {
	r.stopMu.Lock()
	fn := r.stopFn
	r.stopMu.Unlock()
	if fn != nil {
		if r.stats != nil {
			r.stats.Close()
			r.stats = nil
		}
		return fn(ctx)
	}
	if r.stats != nil {
		r.stats.Close()
		r.stats = nil
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
	if h.recv.stats != nil {
		h.recv.stats.AddBytes(len(in))
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
