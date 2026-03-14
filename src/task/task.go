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
	ExecutionModelPool     = "pool"
	ExecutionModelFastPath = "fastpath"
	ExecutionModelChannel  = "channel"
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
	stateMu sync.RWMutex
	Name    string

	Pipelines []*pipeline.Pipeline
	Senders   []sender.Sender

	PoolSize         int
	FastPath         bool
	QueueSize        int
	ChannelQueueSize int
	ExecutionModel   string

	LogPayloadSend bool
	PayloadLogMax  int

	pool *ants.Pool
	ch   chan taskRequest
	wg   sync.WaitGroup

	sendStats *logx.TrafficCounter

	sendersByName map[string]sender.Sender

	accepting atomic.Bool
	inflight  sync.WaitGroup

	inflightCount atomic.Int64
}

type taskRequest struct {
	ctx context.Context
	pkt *packet.Packet
}

// Start 初始化任务执行模型。
func (t *Task) Start() error {
	if t.PoolSize <= 0 {
		// 默认采用 4096，优先提升发送侧受限场景吞吐。
		t.PoolSize = 4096
	}
	if t.QueueSize <= 0 {
		t.QueueSize = 8192
	}
	if t.ChannelQueueSize <= 0 {
		t.ChannelQueueSize = t.QueueSize
	}
	mode := t.resolveExecutionModel()
	t.ExecutionModel = mode

	switch mode {
	case ExecutionModelPool:
		p, err := ants.NewPool(
			t.PoolSize,
			ants.WithMaxBlockingTasks(t.QueueSize),
			ants.WithPreAlloc(true),
		)
		if err != nil {
			return err
		}
		t.pool = p
	case ExecutionModelChannel:
		t.ch = make(chan taskRequest, t.ChannelQueueSize)
		t.wg.Add(1)
		go t.channelWorker()
	}

	t.rebuildSenderIndex()
	t.accepting.Store(true)
	if t.sendStats == nil && logx.Enabled(zapcore.InfoLevel) {
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
func (t *Task) Handle(ctx context.Context, pkt *packet.Packet) {
	if !t.accepting.Load() {
		pkt.Release()
		return
	}

	t.inflightCount.Add(1)
	t.inflight.Add(1)
	run := func(runCtx context.Context) {
		defer t.inflight.Done()
		defer t.inflightCount.Add(-1)
		defer pkt.Release()
		t.processAndSend(runCtx, pkt)
	}

	switch t.ExecutionModel {
	case ExecutionModelFastPath:
		run(ctx)
		return
	case ExecutionModelPool:
		if err := t.pool.Submit(func() { run(ctx) }); err != nil {
			t.inflight.Done()
			t.inflightCount.Add(-1)
			pkt.Release()
			logx.L().Errorw("task dropped packet due to full worker pool queue", "task", t.Name, "pool_size", t.PoolSize, "queue_size", t.QueueSize, "error", err)
			return
		}
		return
	case ExecutionModelChannel:
		select {
		case t.ch <- taskRequest{ctx: ctx, pkt: pkt}:
			return
		case <-ctx.Done():
			t.inflight.Done()
			t.inflightCount.Add(-1)
			pkt.Release()
			logx.L().Warnw("task dropped packet due to canceled context while enqueueing channel task", "task", t.Name, "queue_size", t.ChannelQueueSize, "error", ctx.Err())
			return
		}
	default:
		t.inflight.Done()
		t.inflightCount.Add(-1)
		pkt.Release()
		logx.L().Errorw("task dropped packet due to unknown execution model", "task", t.Name, "execution_model", t.ExecutionModel)
		return
	}
}

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

func (t *Task) resolveExecutionModel() string {
	if t.ExecutionModel == "" {
		if t.FastPath {
			return ExecutionModelFastPath
		}
		return ExecutionModelPool
	}
	switch t.ExecutionModel {
	case ExecutionModelFastPath, ExecutionModelPool, ExecutionModelChannel:
		return t.ExecutionModel
	default:
		if t.FastPath {
			return ExecutionModelFastPath
		}
		return ExecutionModelPool
	}
}

// ReuseTrafficCounter 允许在任务重建时复用已有聚合统计对象。
func (t *Task) ReuseTrafficCounter(counter *logx.TrafficCounter) {
	if t == nil || counter == nil {
		return
	}
	t.sendStats = counter
}

// DetachTrafficCounter 从任务实例上分离统计对象，交由新任务继续复用。
func (t *Task) DetachTrafficCounter() *logx.TrafficCounter {
	if t == nil {
		return nil
	}
	c := t.sendStats
	t.sendStats = nil
	return c
}

// StopGraceful 关闭接收入口并等待在途任务完成，再释放资源池。
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
		close(ch)
		t.wg.Wait()
		t.stateMu.Lock()
		if t.ch == ch {
			t.ch = nil
		}
		t.stateMu.Unlock()
	}
	if sendStats != nil {
		sendStats.Close()
	}
	t.sendersByName = nil
}

// processAndSend 依次执行 pipeline，再将结果发送到所有 sender。
func (t *Task) processAndSend(ctx context.Context, pkt *packet.Packet) {
	for _, pl := range t.Pipelines {
		if !pl.Process(pkt) {
			return
		}
	}
	if pkt.Meta.RouteSender != "" {
		if s, ok := t.sendersByName[pkt.Meta.RouteSender]; ok {
			t.sendToSender(ctx, pkt, s)
			return
		}
		logx.L().Warnw("route sender not found in task", "task", t.Name, "route_sender", pkt.Meta.RouteSender)
		return
	}
	for _, s := range t.Senders {
		t.sendToSender(ctx, pkt, s)
	}
}

func payloadHex(b []byte, max int) string {
	if len(b) == 0 {
		return ""
	}
	if max <= 0 {
		max = 256
	}
	if len(b) > max {
		return hex.EncodeToString(b[:max]) + "...(truncated)"
	}
	return hex.EncodeToString(b)
}

func (t *Task) runtimeStats() logx.TaskRuntimeStats {
	stats := logx.TaskRuntimeStats{
		PoolSize:  t.PoolSize,
		QueueSize: t.QueueSize,
		Inflight:  t.inflightCount.Load(),
		FastPath:  t.ExecutionModel == ExecutionModelFastPath,
	}
	t.stateMu.RLock()
	pool := t.pool
	ch := t.ch
	t.stateMu.RUnlock()
	if pool != nil {
		stats.PoolRunning = pool.Running()
		stats.PoolFree = pool.Free()
		stats.PoolWaiting = pool.Waiting()
		if stats.QueueSize > 0 {
			stats.QueueAvailable = stats.QueueSize - stats.PoolWaiting
			if stats.QueueAvailable < 0 {
				stats.QueueAvailable = 0
			}
		}
	}
	if ch != nil && t.ChannelQueueSize > 0 {
		stats.QueueSize = t.ChannelQueueSize
		stats.PoolWaiting = len(ch)
		stats.QueueAvailable = cap(ch) - len(ch)
		if stats.QueueAvailable < 0 {
			stats.QueueAvailable = 0
		}
	}
	return stats
}

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

func (t *Task) sendToSender(ctx context.Context, pkt *packet.Packet, s sender.Sender) {
	if t.sendStats != nil {
		t.sendStats.AddBytes(len(pkt.Payload))
	}
	if t.LogPayloadSend && logx.Enabled(zapcore.InfoLevel) {
		logx.L().Infow("task payload send",
			"task", t.Name,
			"sender", s.Name(),
			"kind", pkt.Kind,
			"payload_len", len(pkt.Payload),
			"payload_hex", payloadHex(pkt.Payload, t.PayloadLogMax),
			"transfer_id", pkt.Meta.TransferID,
			"offset", pkt.Meta.Offset,
			"total_size", pkt.Meta.TotalSize,
			"eof", pkt.Meta.EOF,
		)
	}
	if err := s.Send(ctx, pkt); err != nil {
		logx.L().Errorw("sender send error", "task", t.Name, "sender", s.Name(), "error", err)
	}
}
