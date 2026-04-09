// gnet_udp.go 实现基于 gnet 的 UDP 接收端。
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

type GnetUDP struct {
	// name 是 receiver 配置名，用于 runtime 索引、日志与聚合统计标签。
	name string
	// listen 是规范化后的 gnet 监听地址（带协议前缀），由 Start 直接传给 gnet.Run。
	listen string
	// multicore / numEventLoop / readBufferCap / socketRecvBuffer 共同决定 gnet 事件循环的并发与 socket 行为。
	multicore        bool
	numEventLoop     int
	readBufferCap    int
	socketRecvBuffer int
	// gnetLogLevel 是映射后的 gnet 内部日志级别。
	gnetLogLevel logging.Level
	// matchKeyBuilder / matchKeyMode 是初始化阶段编译好的 match key 逻辑与模式名。
	matchKeyBuilder udpMatchKeyBuilder
	matchKeyMode    string

	// onPacket 是 runtime 注入的分发回调；每收到一个 UDP 数据报就调用一次。
	onPacket func(*packet.Packet)

	// stopMu + stopFn 保存 gnet 引擎暴露的停止函数，供热重载/停机路径调用。
	stopMu sync.Mutex
	stopFn func(context.Context) error
	// stats 是 receiver 聚合统计句柄，采用 atomic.Pointer 以适应 OnTraffic 与 Stop 并发访问。
	stats atomic.Pointer[logx.TrafficCounter]
}

// NewGnetUDP 构造基于 gnet 的 UDP 接收端。
// 该对象本身只完成参数归一化；真正的监听与统计句柄创建发生在 Start 阶段。
func NewGnetUDP(name, listen string, multicore bool, numEventLoop, readBufferCap, socketRecvBuffer int, gnetLogLevel string, matchKey config.ReceiverMatchKeyConfig) (*GnetUDP, error) {
	builder, mode, err := compileUDPMatchKeyBuilder(matchKey)
	if err != nil {
		return nil, err
	}
	return &GnetUDP{
		name:             name,
		listen:           normalizeGnetListen("udp", listen),
		multicore:        multicore,
		numEventLoop:     numEventLoop,
		readBufferCap:    readBufferCap,
		socketRecvBuffer: socketRecvBuffer,
		gnetLogLevel:     logx.ParseGnetLogLevel(gnetLogLevel),
		matchKeyBuilder:  builder,
		matchKeyMode:     mode,
	}, nil
}

// Name 返回 receiver 名称，用于 runtime 和聚合统计日志关联同一个配置实体。
func (r *GnetUDP) Name() string { return r.name }

// Key 返回 receiver 的稳定复用键。
// 热重载比较配置时会结合该键理解当前实例的监听身份。
func (r *GnetUDP) Key() string { return "udp_gnet|" + r.listen }

// MatchKeyMode 返回 UDP receiver 当前生效的 match key 模式。
func (r *GnetUDP) MatchKeyMode() string { return r.matchKeyMode }

// Start 启动 UDP gnet 事件循环，并把每个入站报文包装成 packet 后交给 runtime。
//
// 聚合统计关系：
//   - Start 时在 info 级别下创建 receiver traffic stats；
//   - OnTraffic 命中每个数据报时调用 AddBytes；
//   - Start 退出或 Stop 触发时关闭统计句柄。
func (r *GnetUDP) Start(ctx context.Context, onPacket func(*packet.Packet)) error {
	r.onPacket = onPacket
	if logx.Enabled(zapcore.InfoLevel) {
		r.stats.Store(logx.AcquireTrafficCounter(
			"receiver traffic stats",
			"role", "receiver",
			"receiver", r.Name(),
			"receiver_key", r.Key(),
			"proto", "udp",
		))
	}
	defer func() {
		if stats := r.stats.Swap(nil); stats != nil {
			stats.Close()
		}
	}()
	return gnet.Run(
		&udpHandler{recv: r},
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

// Stop 请求 gnet 引擎退出，并提前关闭 receiver 聚合统计句柄。
// stats 使用 Swap(nil) 是为了避免与 Start defer 或 OnTraffic 并发时重复 Close。
func (r *GnetUDP) Stop(ctx context.Context) error {
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

type udpHandler struct {
	gnet.BuiltinEventEngine
	// recv 指向所属的 GnetUDP，用于访问停止函数、统计句柄和 onPacket 回调。
	recv *GnetUDP
}

// OnBoot 在 gnet 引擎完成启动时记录 eng.Stop，供外部优雅停机。
func (h *udpHandler) OnBoot(eng gnet.Engine) (action gnet.Action) {
	h.recv.stopMu.Lock()
	h.recv.stopFn = eng.Stop
	h.recv.stopMu.Unlock()
	return gnet.None
}

// OnTraffic 处理单次 UDP 可读事件。
//
// 关键流程：
// 1. 使用 Peek+Discard 读取完整数据报，规避平台差异；
// 2. 追加 receiver 聚合统计；
// 3. 复制 payload，构造 match key；
// 4. 调用 runtime 注入的 onPacket，正式进入 selector/task 转发链路。
func (h *udpHandler) OnTraffic(c gnet.Conn) gnet.Action {
	// 对 UDP 场景优先 Peek + Discard，避免在 Linux epoll 模式下直接 Next
	// 触发底层读指针推进后再复制所带来的边界差异（表现为上层“像是丢头”）。
	in, _ := c.Peek(-1)
	if len(in) == 0 {
		return gnet.None
	}
	_, _ = c.Discard(len(in))
	if stats := h.recv.stats.Load(); stats != nil {
		stats.AddBytes(len(in))
	}
	payload, rel := packet.CopyFrom(in)
	remoteAddr := c.RemoteAddr()
	localAddr := c.LocalAddr()
	remote := addrString(remoteAddr)
	local := addrString(localAddr)
	matchKey := h.recv.matchKeyBuilder(remote, local, remoteAddr, localAddr)
	h.recv.onPacket(&packet.Packet{
		Envelope: packet.Envelope{
			Kind:    packet.PayloadKindStream,
			Payload: payload,
			Meta: packet.Meta{
				Proto:        packet.ProtoUDP,
				ReceiverName: h.recv.name,
				Remote:       remote,
				Local:        local,
				MatchKey:     matchKey,
			},
		},
		ReleaseFn: rel,
	})
	return gnet.None
}

func addrString(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	return addr.String()
}
