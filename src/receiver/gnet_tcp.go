// gnet_tcp.go 实现基于 gnet 的 TCP 接收端。
package receiver

import (
	"context"
	"net/netip"
	"sync"
	"sync/atomic"

	"forward-stub/src/logx"
	"forward-stub/src/packet"

	"github.com/panjf2000/gnet/v2"
	"github.com/panjf2000/gnet/v2/pkg/logging"
	"go.uber.org/zap/zapcore"
)

// GnetTCP describes receiver-level state used by the forwarding architecture.
type GnetTCP struct {
	name             string
	listen           string
	multicore        bool
	numEventLoop     int
	readBufferCap    int
	socketRecvBuffer int
	framer           Framer
	gnetLogLevel     logging.Level

	onPacket func(*packet.Packet)

	stopMu sync.Mutex
	stopFn func(context.Context) error
	stats  atomic.Pointer[logx.TrafficCounter]
}

// NewGnetTCP 负责该函数对应的核心逻辑，详见实现细节。
func NewGnetTCP(name, listen string, multicore bool, numEventLoop, readBufferCap, socketRecvBuffer int, framer Framer, gnetLogLevel string) *GnetTCP {
	return &GnetTCP{
		name:             name,
		listen:           listen,
		multicore:        multicore,
		numEventLoop:     numEventLoop,
		readBufferCap:    readBufferCap,
		socketRecvBuffer: socketRecvBuffer,
		framer:           framer,
		gnetLogLevel:     logx.ParseGnetLogLevel(gnetLogLevel),
	}
}

// Name 负责该函数对应的核心逻辑，详见实现细节。
func (r *GnetTCP) Name() string { return r.name }

// Key 负责该函数对应的核心逻辑，详见实现细节。
func (r *GnetTCP) Key() string { return "tcp_gnet|" + r.listen }

// Start 负责该函数对应的核心逻辑，详见实现细节。
func (r *GnetTCP) Start(ctx context.Context, onPacket func(*packet.Packet)) error {
	r.onPacket = onPacket
	if logx.Enabled(zapcore.InfoLevel) {
		r.stats.Store(logx.AcquireTrafficCounter(
			"receiver traffic stats",
			"role", "receiver",
			"receiver", r.Name(),
			"receiver_key", r.Key(),
			"proto", "tcp",
		))
	}
	defer func() {
		if stats := r.stats.Swap(nil); stats != nil {
			stats.Close()
		}
	}()
	return gnet.Run(
		&tcpHandler{recv: r},
		r.listen,
		gnet.WithMulticore(r.multicore),
		gnet.WithNumEventLoop(r.numEventLoop),
		gnet.WithReadBufferCap(r.readBufferCap),
		gnet.WithSocketRecvBuffer(r.socketRecvBuffer),
		gnet.WithReusePort(true),
		gnet.WithReuseAddr(true),
		gnet.WithLogLevel(r.gnetLogLevel),
	)
}

// Stop 负责该函数对应的核心逻辑，详见实现细节。
func (r *GnetTCP) Stop(ctx context.Context) error {
	r.stopMu.Lock()
	fn := r.stopFn
	r.stopMu.Unlock()
	if fn != nil {
		if stats := r.stats.Swap(nil); stats != nil {
			stats.Close()
		}
		return fn(ctx)
	}
	if stats := r.stats.Swap(nil); stats != nil {
		stats.Close()
	}
	return nil
}

// connState stores package-local state used by gnet_tcp.go.
type connState struct {
	buf        []byte
	remote     string
	local      string
	srcIPv4    uint32
	srcPort    uint16
	hasSrcAddr bool
}

// tcpHandler stores package-local state used by gnet_tcp.go.
type tcpHandler struct {
	gnet.BuiltinEventEngine
	recv *GnetTCP
}

// OnBoot 负责该函数对应的核心逻辑，详见实现细节。
func (h *tcpHandler) OnBoot(eng gnet.Engine) (action gnet.Action) {
	h.recv.stopMu.Lock()
	h.recv.stopFn = eng.Stop
	h.recv.stopMu.Unlock()
	return gnet.None
}

// OnOpen 负责该函数对应的核心逻辑，详见实现细节。
func (h *tcpHandler) OnOpen(c gnet.Conn) (out []byte, action gnet.Action) {
	state := &connState{
		buf:    make([]byte, 0, 4096),
		remote: c.RemoteAddr().String(),
		local:  c.LocalAddr().String(),
	}
	if addrPort, err := netip.ParseAddrPort(state.remote); err == nil {
		meta := packet.Meta{}
		meta.SetSourceAddrPort(addrPort)
		state.srcIPv4 = meta.SrcIPv4
		state.srcPort = meta.SrcPort
		state.hasSrcAddr = meta.HasSrcAddr
	} else {
		meta := packet.Meta{}
		meta.SetSourceFromRemote(state.remote)
		state.srcIPv4 = meta.SrcIPv4
		state.srcPort = meta.SrcPort
		state.hasSrcAddr = meta.HasSrcAddr
	}
	c.SetContext(state)
	return nil, gnet.None
}

// OnTraffic 负责该函数对应的核心逻辑，详见实现细节。
func (h *tcpHandler) OnTraffic(c gnet.Conn) gnet.Action {
	// 与 UDP 侧保持一致：先 Peek 再 Discard，规避不同平台/事件循环下读取游标推进时机差异。
	in, _ := c.Peek(-1)
	if len(in) == 0 {
		return gnet.None
	}
	_, _ = c.Discard(len(in))
	cs := c.Context().(*connState)
	cs.buf = append(cs.buf, in...)

	if h.recv.framer == nil {
		if stats := h.recv.stats.Load(); stats != nil {
			stats.AddBytes(len(cs.buf))
		}
		payload, rel := packet.CopyFrom(cs.buf)
		cs.buf = cs.buf[:0]
		meta := packet.Meta{Proto: packet.ProtoTCP, Remote: cs.remote, Local: cs.local, SrcIPv4: cs.srcIPv4, SrcPort: cs.srcPort, HasSrcAddr: cs.hasSrcAddr}
		h.recv.onPacket(&packet.Packet{
			Envelope: packet.Envelope{
				Kind:    packet.PayloadKindStream,
				Payload: payload,
				Meta:    meta,
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
		if stats := h.recv.stats.Load(); stats != nil {
			stats.AddBytes(len(fr))
		}
		payload, rel := packet.CopyFrom(fr)
		meta := packet.Meta{Proto: packet.ProtoTCP, Remote: cs.remote, Local: cs.local, SrcIPv4: cs.srcIPv4, SrcPort: cs.srcPort, HasSrcAddr: cs.hasSrcAddr}
		h.recv.onPacket(&packet.Packet{
			Envelope: packet.Envelope{
				Kind:    packet.PayloadKindStream,
				Payload: payload,
				Meta:    meta,
			},
			ReleaseFn: rel,
		})
	}
	return gnet.None
}
