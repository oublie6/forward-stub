// gnet_tcp.go 实现基于 gnet 的 TCP 接收端。
package receiver

import (
	"context"
	"net"
	"sync"
	"sync/atomic"

	"forward-stub/src/config"
	"forward-stub/src/logx"
	"forward-stub/src/packet"

	"github.com/panjf2000/gnet/v2"
	"github.com/panjf2000/gnet/v2/pkg/logging"
	"go.uber.org/zap/zapcore"
)

type GnetTCP struct {
	// name / listen 是 runtime 用于索引与复用 receiver 的主标识。
	name   string
	listen string
	// gnet 相关参数控制事件循环并发与 socket 缓冲行为。
	multicore        bool
	numEventLoop     int
	readBufferCap    int
	socketRecvBuffer int
	// framer 负责把 TCP 字节流切成业务帧；nil 时表示“收到多少发多少”。
	framer Framer
	// gnetLogLevel 是项目日志级别映射到 gnet 的结果。
	gnetLogLevel logging.Level
	// matchKeyBuilder / matchKeyMode 是初始化阶段编译好的 match key 逻辑与模式。
	matchKeyBuilder tcpMatchKeyBuilder
	matchKeyMode    string

	// onPacket 是 runtime 注入的 packet 分发回调。
	onPacket func(*packet.Packet)

	// stopMu + stopFn 保存引擎停止入口。
	stopMu sync.Mutex
	stopFn func(context.Context) error
	// stats 是 TCP receiver 的聚合统计句柄，OnTraffic 热路径会无锁读取它。
	stats atomic.Pointer[logx.TrafficCounter]
}

// NewGnetTCP 构造基于 gnet 的 TCP 接收端，并固化 framing 策略。
func NewGnetTCP(name, listen string, multicore bool, numEventLoop, readBufferCap, socketRecvBuffer int, framer Framer, gnetLogLevel string, matchKey config.ReceiverMatchKeyConfig) (*GnetTCP, error) {
	builder, mode, err := compileTCPMatchKeyBuilder(matchKey)
	if err != nil {
		return nil, err
	}
	return &GnetTCP{
		name:             name,
		listen:           normalizeGnetListen("tcp", listen),
		multicore:        multicore,
		numEventLoop:     numEventLoop,
		readBufferCap:    readBufferCap,
		socketRecvBuffer: socketRecvBuffer,
		framer:           framer,
		gnetLogLevel:     logx.ParseGnetLogLevel(gnetLogLevel),
		matchKeyBuilder:  builder,
		matchKeyMode:     mode,
	}, nil
}

// Name 返回 receiver 配置名。
func (r *GnetTCP) Name() string { return r.name }

// Key 返回 TCP receiver 的稳定身份键。
func (r *GnetTCP) Key() string { return "tcp_gnet|" + r.listen }

// MatchKeyMode 返回 TCP receiver 当前生效的 match key 模式。
func (r *GnetTCP) MatchKeyMode() string { return r.matchKeyMode }

// Start 启动 TCP gnet 引擎并建立 receiver 侧聚合统计。
// 与 UDP 类似，统计句柄的生命周期与 gnet.Run 一致。
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

// Stop 请求 TCP gnet 引擎退出，并关闭统计句柄。
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

type connState struct {
	// buf 保存该连接尚未被 framer 消费的剩余字节。
	buf []byte
	// remote / local 缓存地址字符串，避免每帧重复格式化。
	remote string
	local  string
	// remoteAddr / localAddr 保留原始地址对象，供需要提取 IP 或端口的模式复用。
	remoteAddr net.Addr
	localAddr  net.Addr
	// matchKey 在连接建立时预先生成，后续每帧直接复用。
	matchKey string
}

type tcpHandler struct {
	gnet.BuiltinEventEngine
	// recv 指向所属 receiver，用于调用 framer、统计器和 onPacket。
	recv *GnetTCP
}

// OnBoot 记录 gnet 停止函数，供 Stop 使用。
func (h *tcpHandler) OnBoot(eng gnet.Engine) (action gnet.Action) {
	h.recv.stopMu.Lock()
	h.recv.stopFn = eng.Stop
	h.recv.stopMu.Unlock()
	return gnet.None
}

// OnOpen 为每个 TCP 连接初始化独立 connState。
// 这样后续分帧时可以在连接维度保留残留字节与地址信息。
func (h *tcpHandler) OnOpen(c gnet.Conn) (out []byte, action gnet.Action) {
	remoteAddr := c.RemoteAddr()
	localAddr := c.LocalAddr()
	cs := &connState{
		buf:        make([]byte, 0, 4096),
		remote:     addrString(remoteAddr),
		local:      addrString(localAddr),
		remoteAddr: remoteAddr,
		localAddr:  localAddr,
	}
	cs.matchKey = h.recv.matchKeyBuilder(cs)
	c.SetContext(cs)
	return nil, gnet.None
}

// OnTraffic 处理某个 TCP 连接上的可读事件。
//
// 与 UDP 不同，TCP 是流式协议，因此这里需要：
// 1. 把新读到的字节追加到连接缓冲；
// 2. 若无 framer，则整段缓冲一次性作为一个 packet；
// 3. 若有 framer，则循环切帧、逐帧记统计并投递。
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
		// 无 framer 时，当前缓冲区即视为一个完整 payload。
		if stats := h.recv.stats.Load(); stats != nil {
			stats.AddBytes(len(cs.buf))
		}
		payload, rel := packet.CopyFrom(cs.buf)
		cs.buf = cs.buf[:0]
		h.recv.onPacket(&packet.Packet{
			Envelope: packet.Envelope{
				Kind:    packet.PayloadKindStream,
				Payload: payload,
				Meta: packet.Meta{
					Proto:        packet.ProtoTCP,
					ReceiverName: h.recv.name,
					Remote:       cs.remote,
					Local:        cs.local,
					MatchKey:     cs.matchKey,
				},
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
		// 多帧场景下每个 frame 都单独记一次 receiver 统计并生成独立 packet。
		if stats := h.recv.stats.Load(); stats != nil {
			stats.AddBytes(len(fr))
		}
		payload, rel := packet.CopyFrom(fr)
		h.recv.onPacket(&packet.Packet{
			Envelope: packet.Envelope{
				Kind:    packet.PayloadKindStream,
				Payload: payload,
				Meta: packet.Meta{
					Proto:        packet.ProtoTCP,
					ReceiverName: h.recv.name,
					Remote:       cs.remote,
					Local:        cs.local,
					MatchKey:     cs.matchKey,
				},
			},
			ReleaseFn: rel,
		})
	}
	return gnet.None
}
