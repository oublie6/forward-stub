// gnet_udp.go 实现基于 gnet 的 UDP 接收端。
package receiver

import (
	"context"
	"sync"

	"forward-stub/src/logx"
	"forward-stub/src/packet"

	"github.com/panjf2000/gnet/v2"
	"github.com/panjf2000/gnet/v2/pkg/logging"
	"go.uber.org/zap/zapcore"
)

type GnetUDP struct {
	name          string
	listen        string
	multicore     bool
	numEventLoop  int
	readBufferCap int
	gnetLogLevel  logging.Level

	onPacket func(*packet.Packet)

	stopMu sync.Mutex
	stopFn func(context.Context) error
	stats  *logx.TrafficCounter
}

// NewGnetUDP 负责该函数对应的核心逻辑，详见实现细节。
func NewGnetUDP(name, listen string, multicore bool, numEventLoop, readBufferCap int, gnetLogLevel string) *GnetUDP {
	return &GnetUDP{
		name:          name,
		listen:        listen,
		multicore:     multicore,
		numEventLoop:  numEventLoop,
		readBufferCap: readBufferCap,
		gnetLogLevel:  logx.ParseGnetLogLevel(gnetLogLevel),
	}
}

// Name 负责该函数对应的核心逻辑，详见实现细节。
func (r *GnetUDP) Name() string { return r.name }

// Key 负责该函数对应的核心逻辑，详见实现细节。
func (r *GnetUDP) Key() string { return "udp_gnet|" + r.listen }

// Start 负责该函数对应的核心逻辑，详见实现细节。
func (r *GnetUDP) Start(ctx context.Context, onPacket func(*packet.Packet)) error {
	r.onPacket = onPacket
	if logx.Enabled(zapcore.InfoLevel) {
		r.stats = logx.AcquireTrafficCounter(
			"receiver traffic stats",
			"role", "receiver",
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
		gnet.WithNumEventLoop(r.numEventLoop),
		gnet.WithReadBufferCap(r.readBufferCap),
		gnet.WithReusePort(true),
		gnet.WithReuseAddr(true),
		gnet.WithLogLevel(r.gnetLogLevel),
	)
}

// Stop 负责该函数对应的核心逻辑，详见实现细节。
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

// OnBoot 负责该函数对应的核心逻辑，详见实现细节。
func (h *udpHandler) OnBoot(eng gnet.Engine) (action gnet.Action) {
	h.recv.stopMu.Lock()
	h.recv.stopFn = eng.Stop
	h.recv.stopMu.Unlock()
	return gnet.None
}

// OnTraffic 负责该函数对应的核心逻辑，详见实现细节。
func (h *udpHandler) OnTraffic(c gnet.Conn) gnet.Action {
	// 对 UDP 场景优先 Peek + Discard，避免在 Linux epoll 模式下直接 Next
	// 触发底层读指针推进后再复制所带来的边界差异（表现为上层“像是丢头”）。
	in, _ := c.Peek(-1)
	if len(in) == 0 {
		return gnet.None
	}
	_, _ = c.Discard(len(in))
	if h.recv.stats != nil {
		h.recv.stats.AddBytes(len(in))
	}
	payload, rel := packet.CopyFrom(in)
	h.recv.onPacket(&packet.Packet{
		Envelope: packet.Envelope{
			Kind:    packet.PayloadKindStream,
			Payload: payload,
			Meta: packet.Meta{
				Proto:  packet.ProtoUDP,
				Remote: c.RemoteAddr().String(),
				Local:  c.LocalAddr().String(),
			},
		},
		ReleaseFn: rel,
	})
	return gnet.None
}
