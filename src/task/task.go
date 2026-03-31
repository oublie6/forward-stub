// task.go 定义单条处理任务，串联 pipeline 执行与 sender 分发。
package task

import (
	"context"
	"encoding/hex"
	"sync"
	"sync/atomic"

	"forward-stub/src/logx"
	"forward-stub/src/packet"
	"forward-stub/src/pipeline"
	"forward-stub/src/sender"

	"github.com/panjf2000/ants/v2"
	"go.uber.org/zap/zapcore"
)

const (
	// ExecutionModelPool 表示通过 ants worker pool 异步处理 packet。
	// 适合需要限制并发和提高吞吐的常规场景，也是默认模型。
	ExecutionModelPool = "pool"
	// ExecutionModelFastPath 表示在当前 goroutine 中同步执行 pipeline+sender。
	// 延迟最低，但会把下游耗时直接反馈到 receiver 分发热路径。
	ExecutionModelFastPath = "fastpath"
	// ExecutionModelChannel 表示进入单 goroutine 顺序 worker。
	// 适合需要保持顺序或限制串行副作用的任务。
	ExecutionModelChannel = "channel"

	defaultPoolSize         = 4096
	defaultChannelQueueSize = 8192
)

// Task 表示一条完整数据处理链路：
// input packet -> pipelines -> senders。
//
// 运行模型：
//  1. fastpath：当前 goroutine 内同步执行，低延迟；
//  2. pool：走 ants worker pool（高吞吐、可控并发）；
//  3. channel：单 goroutine + 有界 channel，顺序处理；
//  4. 停机时通过 accepting + inflight 保证优雅退出。
type Task struct {
	// stateMu 保护可在运行时切换的执行资源（pool/ch/sendStats）。
	// 热路径读取 runtimeStats 时只持读锁，尽量降低 StopGraceful 并发影响。
	stateMu sync.RWMutex
	// Name 是 task 配置名，也是聚合统计日志、热重载复用和运维排障的主标识。
	Name string

	// Pipelines 是该 task 按顺序执行的处理链。
	// 每个 packet 在真正发送前都会依次跑完这些 stage；任何 stage 返回 false 都会终止转发。
	Pipelines []*pipeline.Pipeline
	// Senders 是 task 命中后需要投递的发送端集合。
	// 若 pipeline 通过 RouteSender 指定了目标 sender，则只会命中其中一个。
	Senders []sender.Sender

	// PoolSize 是 pool 模型的 worker 数；小于等于 0 时 Start 会回退到默认值。
	PoolSize int
	// ChannelQueueSize 是 channel 模型的有界缓冲大小；未配置时回退到默认值。
	ChannelQueueSize int
	// ExecutionModel 是最终生效的执行模型，Start 后会被 resolveExecutionModel 规范化。
	ExecutionModel string

	// LogPayloadSend 控制 task 发送阶段是否打印 payload 摘要日志。
	// 该开关只影响观测行为，不影响转发结果。
	LogPayloadSend bool
	// PayloadLogMax 是发送 payload 摘要的最大字节数；仅在 LogPayloadSend=true 时生效。
	PayloadLogMax int

	// pool / ch / wg 是三种执行模型对应的运行时资源。
	// 它们只在 Start/StopGraceful 或 runtimeStats 中访问。
	pool *ants.Pool
	ch   chan taskRequest
	wg   sync.WaitGroup

	// sendStats 是 task 发送聚合统计句柄。
	// 热重载时可从旧 task 分离并交给新 task 复用，避免“task send traffic stats”断档。
	sendStats *logx.TrafficCounter

	// sendersByName 支持 pipeline 通过 Meta.RouteSender 指定单个 sender。
	// Start 时构建，避免热路径每次遍历 Senders 做线性查找。
	sendersByName map[string]sender.Sender

	// accepting 控制是否仍接受新包进入任务。
	// StopGraceful 先把它设为 false，再等待 inflight 清零，实现平滑下线。
	accepting atomic.Bool
	// inflight 统计“已被接收但尚未完全处理结束”的请求，用于优雅停机等待。
	inflight sync.WaitGroup

	// inflightCount 是 inflight 的原子镜像，用于聚合统计日志输出当前在途数。
	inflightCount atomic.Int64
}

// taskRequest 是 channel 执行模型在队列中传递的最小工作单元。
// 它把请求上下文和 packet 绑定在一起，确保串行 worker 能完整恢复调用环境。
type taskRequest struct {
	ctx context.Context
	pkt *packet.Packet
}

// Start 初始化 task 的执行模型、发送索引以及聚合统计回调。
//
// 调用时机：
//   - 冷启动时由 runtime.addTask 调用；
//   - 热重载重建 task 时再次调用；
//   - 测试中也会直接构造 Task 后调用。
//
// 线程安全：不应与 StopGraceful 并发调用。
func (t *Task) Start() error {
	if t.PoolSize <= 0 {
		// 默认采用 4096，优先提升发送侧受限场景吞吐。
		t.PoolSize = defaultPoolSize
	}
	if t.ChannelQueueSize <= 0 {
		t.ChannelQueueSize = defaultChannelQueueSize
	}
	mode := t.resolveExecutionModel()
	t.ExecutionModel = mode

	switch mode {
	case ExecutionModelPool:
		// pool 模型下的并发控制完全交给 ants，适合高吞吐发送型链路。
		p, err := ants.NewPool(
			t.PoolSize,
			ants.WithPreAlloc(true),
		)
		if err != nil {
			return err
		}
		t.pool = p
	case ExecutionModelChannel:
		// channel 模型固定一个 goroutine 顺序消费，可天然保证单任务内处理顺序。
		t.ch = make(chan taskRequest, t.ChannelQueueSize)
		t.wg.Add(1)
		go t.channelWorker()
	}

	t.rebuildSenderIndex()
	t.accepting.Store(true)
	if t.sendStats == nil && logx.Enabled(zapcore.InfoLevel) {
		// 只有 info 级别启用时才创建 task 发送统计，避免关闭日志时仍给热路径增加原子写。
		t.sendStats = logx.AcquireTrafficCounter(
			"task send traffic stats",
			"role", "task",
			"task", t.Name,
			"direction", "send",
		)
	}
	logx.RegisterTaskRuntimeStats(t.Name, t.runtimeStats)
	return nil
}

// Handle 接收单个 packet 并提交处理。
// 生命周期约束：谁持有 packet 谁负责 Release。
//
// 并发语义：
//   - 允许多个 goroutine 并发调用；
//   - 一旦 accepting=false，新的 packet 会立即丢弃并释放；
//   - 成功进入 inflight 后，无论后续走哪条分支，都必须确保 Done 与 Release 成对执行。
func (t *Task) Handle(ctx context.Context, pkt *packet.Packet) {
	if !t.accepting.Load() {
		pkt.Release()
		return
	}

	t.inflightCount.Add(1)
	t.inflight.Add(1)
	run := func(runCtx context.Context) {
		// run 是三种执行模型共享的真正业务闭包：
		// 先在退出时归还 inflight 和记忆体，再执行 pipeline+sender。
		defer t.inflight.Done()
		defer t.inflightCount.Add(-1)
		defer pkt.Release()
		t.processAndSend(runCtx, pkt)
	}

	switch t.ExecutionModel {
	case ExecutionModelFastPath:
		// fastpath 直接在当前 goroutine 内执行，没有额外排队/切换成本。
		run(ctx)
		return
	case ExecutionModelPool:
		// pool 模型下 Submit 失败通常意味着协程池已停止或处于异常状态，需要记录丢弃日志并主动释放 packet。
		if err := t.pool.Submit(func() { run(ctx) }); err != nil {
			t.inflight.Done()
			t.inflightCount.Add(-1)
			pkt.Release()
			logx.L().Errorw("任务丢弃数据包：协程池提交失败", "任务名称", t.Name, "协程池大小", t.PoolSize, "错误", err)
			return
		}
		return
	case ExecutionModelChannel:
		select {
		case t.ch <- taskRequest{ctx: ctx, pkt: pkt}:
			return
		case <-ctx.Done():
			// channel 模型在入队前若上游 context 已取消，应立即放弃，避免停机时无意义堆积。
			t.inflight.Done()
			t.inflightCount.Add(-1)
			pkt.Release()
			logx.L().Warnw("任务丢弃数据包：channel入队时上下文已取消", "任务名称", t.Name, "channel队列长度", t.ChannelQueueSize, "错误", ctx.Err())
			return
		}
	default:
		t.inflight.Done()
		t.inflightCount.Add(-1)
		pkt.Release()
		logx.L().Errorw("任务丢弃数据包：执行模型无效", "任务名称", t.Name, "执行模型", t.ExecutionModel)
		return
	}
}

// channelWorker 是 channel 模型的唯一消费协程。
// 它串行处理队列中的 taskRequest，因此天然保证同一 task 内的顺序一致性。
func (t *Task) channelWorker() {
	defer t.wg.Done()
	for req := range t.ch {
		func(r taskRequest) {
			defer t.inflight.Done()
			defer t.inflightCount.Add(-1)
			defer r.pkt.Release()
			t.processAndSend(r.ctx, r.pkt)
		}(req)
	}
}

// resolveExecutionModel 统一解析最终生效的执行模型。
func (t *Task) resolveExecutionModel() string {
	if t.ExecutionModel == "" {
		return ExecutionModelPool
	}
	switch t.ExecutionModel {
	case ExecutionModelFastPath, ExecutionModelPool, ExecutionModelChannel:
		return t.ExecutionModel
	default:
		return ExecutionModelPool
	}
}

// ReuseTrafficCounter 允许在任务热重建时复用旧 task 的发送聚合统计对象。
// 这样同名 task 在配置替换期间仍能持续累计 total_packets/total_bytes，不会因为重建而归零。
func (t *Task) ReuseTrafficCounter(counter *logx.TrafficCounter) {
	if t == nil || counter == nil {
		return
	}
	t.sendStats = counter
}

// DetachTrafficCounter 从当前 task 分离发送统计句柄。
// 调用者通常是 runtime.removeTask，在“先删旧 task、再建同名新 task”时把句柄移交出去。
func (t *Task) DetachTrafficCounter() *logx.TrafficCounter {
	if t == nil {
		return nil
	}
	c := t.sendStats
	t.sendStats = nil
	return c
}

// StopGraceful 平滑停止 task。
//
// 顺序要求：
// 1. 先拒绝新包进入；
// 2. 再等待 inflight 全部结束；
// 3. 注销 runtime stats，防止聚合日志继续看到已停止 task；
// 4. 最后释放 pool/channel/sendStats 等资源。
//
// 线程安全：通常只应由生命周期管理线程调用一次；内部状态释放做了必要的幂等保护。
func (t *Task) StopGraceful() {
	t.accepting.Store(false)
	t.inflight.Wait()
	logx.UnregisterTaskRuntimeStats(t.Name)
	t.stateMu.Lock()
	pool := t.pool
	t.pool = nil
	ch := t.ch
	sendStats := t.sendStats
	t.sendStats = nil
	t.stateMu.Unlock()
	if pool != nil {
		pool.Release()
	}
	if ch != nil {
		// 关闭 channel 前已等待 inflight 归零，因此不会再有新的生产者写入。
		close(ch)
		t.wg.Wait()
		t.stateMu.Lock()
		if t.ch == ch {
			t.ch = nil
		}
		t.stateMu.Unlock()
	}
	if sendStats != nil {
		// 关闭发送聚合统计句柄，必要时由 runtime 在 Stop 前提前 Detach 以继续复用。
		sendStats.Close()
	}
	t.sendersByName = nil
}

// processAndSend 执行 task 的核心业务流程：
// 1. 顺序执行 pipelines；
// 2. 若某个 stage 决定丢弃（返回 false），立即终止；
// 3. 若 pipeline 指定 RouteSender，则只向命中的 sender 投递；
// 4. 否则广播到 task 绑定的全部 sender。
func (t *Task) processAndSend(ctx context.Context, pkt *packet.Packet) {
	for _, pl := range t.Pipelines {
		// pipeline 是 task 内最靠前的处理环节，任何 stage 返回 false 都表示该包到此结束。
		if !pl.Process(pkt) {
			return
		}
	}
	if pkt.Meta.RouteSender != "" {
		// 路由 sender 是单任务内的精确分流能力，可避免把同一包广播给全部 sender。
		if s, ok := t.sendersByName[pkt.Meta.RouteSender]; ok {
			t.sendToSender(ctx, pkt, s)
			return
		}
		logx.L().Warnw("路由发送端未命中任务内发送端", "任务名称", t.Name, "路由发送端", pkt.Meta.RouteSender)
		return
	}
	for _, s := range t.Senders {
		t.sendToSender(ctx, pkt, s)
	}
}

// payloadHex 把 payload 摘要转成十六进制字符串，并在必要时截断。
// 仅服务日志慢路径，避免直接把二进制内容写入结构化日志。
func payloadHex(b []byte, max int) string {
	if len(b) == 0 {
		return ""
	}
	if max <= 0 {
		max = 256
	}
	if len(b) > max {
		return hex.EncodeToString(b[:max]) + "...(已截断)"
	}
	return hex.EncodeToString(b)
}

// runtimeStats 生成当前 task 的运行时快照，供聚合统计日志线程调用。
//
// 并发语义：
//   - 可与 Handle/StopGraceful 并发执行；
//   - 通过 stateMu 只保护 pool/ch 指针读取，不阻塞热路径计数；
//   - 读取结果允许是“某一时刻的近似快照”，用于观测而非强一致控制。
func (t *Task) runtimeStats() logx.TaskRuntimeStats {
	stats := logx.TaskRuntimeStats{
		ExecutionModel: t.ExecutionModel,
		Inflight:       t.inflightCount.Load(),
	}
	t.stateMu.RLock()
	pool := t.pool
	ch := t.ch
	t.stateMu.RUnlock()
	if pool != nil {
		stats.PoolSize = t.PoolSize
		stats.WorkerPoolRunning = pool.Running()
		stats.WorkerPoolFree = pool.Free()
		stats.WorkerPoolWaiting = pool.Waiting()
	}
	if ch != nil && t.ChannelQueueSize > 0 {
		stats.ChannelQueueSize = cap(ch)
		stats.ChannelQueueUsed = len(ch)
		stats.ChannelQueueAvailable = cap(ch) - len(ch)
	}
	return stats
}

// rebuildSenderIndex 重建“sender 名称 -> sender 实例”索引。
// 仅在 Start/热重建阶段调用，避免 RouteSender 热路径线性扫描。
func (t *Task) rebuildSenderIndex() {
	if len(t.Senders) == 0 {
		t.sendersByName = nil
		return
	}
	idx := make(map[string]sender.Sender, len(t.Senders))
	for _, s := range t.Senders {
		if s == nil {
			continue
		}
		idx[s.Name()] = s
	}
	t.sendersByName = idx
}

// sendToSender 执行一次真正的 sender.Send 调用，并补充发送侧观测。
//
// 该函数位于 task 热路径末端：
//   - 先记聚合统计；
//   - 按需打印 payload 摘要；
//   - 最后调用 sender 发送。
func (t *Task) sendToSender(ctx context.Context, pkt *packet.Packet, s sender.Sender) {
	if t.sendStats != nil {
		t.sendStats.AddBytes(len(pkt.Payload))
	}
	if t.LogPayloadSend && logx.Enabled(zapcore.InfoLevel) {
		logx.L().Infow("任务发送Payload摘要",
			"任务名称", t.Name,
			"发送端", s.Name(),
			"载荷类型", pkt.Kind,
			"Payload长度", len(pkt.Payload),
			"Payload十六进制", payloadHex(pkt.Payload, t.PayloadLogMax),
			"传输ID", pkt.Meta.TransferID,
			"偏移量", pkt.Meta.Offset,
			"总大小", pkt.Meta.TotalSize,
			"是否EOF", pkt.Meta.EOF,
		)
	}
	if err := s.Send(ctx, pkt); err != nil {
		logx.L().Errorw("发送端发送失败", "任务名称", t.Name, "发送端", s.Name(), "错误", err)
	}
}
