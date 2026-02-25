// task.go 定义单条处理任务，串联 pipeline 执行与 sender 分发。
package task

import (
	"context"
	"sync"
	"sync/atomic"

	"forword-stub/src/logx"
	"forword-stub/src/packet"
	"forword-stub/src/pipeline"
	"forword-stub/src/sender"

	"github.com/panjf2000/ants/v2"
	"go.uber.org/zap/zapcore"
)

// Task 表示一条完整数据处理链路：
// input packet -> pipelines -> senders。
//
// 运行模型：
//  1. 可选 FastPath（当前 goroutine 内同步执行，低延迟）；
//  2. 默认走 ants worker pool（高吞吐、可控并发）；
//  3. 停机时通过 accepting + inflight 保证优雅退出。
type Task struct {
	Name string

	Pipelines []*pipeline.Pipeline
	Senders   []sender.Sender

	PoolSize int
	FastPath bool

	pool  *ants.Pool
	stats *logx.TrafficCounter

	accepting atomic.Bool
	inflight  sync.WaitGroup
}

// Start 初始化任务执行池。
// 使用稳定第三方库 ants：
//   - WithNonblocking(true)：池满时立即返回错误，避免调用方阻塞；
//   - WithPreAlloc(true)：预分配 worker 队列内存，减少高峰时动态扩容抖动。
func (t *Task) Start() error {
	if t.PoolSize <= 0 {
		t.PoolSize = 64
	}
	p, err := ants.NewPool(
		t.PoolSize,
		ants.WithNonblocking(true),
		ants.WithPreAlloc(true),
	)
	if err != nil {
		return err
	}
	t.pool = p
	t.accepting.Store(true)
	if logx.Enabled(zapcore.InfoLevel) {
		t.stats = logx.AcquireTrafficCounter(
			"sender traffic stats",
			"task", t.Name,
		)
	}
	return nil
}

// Handle 接收单个 packet 并提交处理。
// 生命周期约束：谁持有 packet 谁负责 Release。
func (t *Task) Handle(ctx context.Context, pkt *packet.Packet) {
	if !t.accepting.Load() {
		pkt.Release()
		return
	}

	t.inflight.Add(1)
	run := func() {
		defer t.inflight.Done()
		defer pkt.Release()
		t.processAndSend(ctx, pkt)
	}

	if t.FastPath {
		run()
		return
	}

	if err := t.pool.Submit(run); err != nil {
		t.inflight.Done()
		pkt.Release()
		if logx.Enabled(zapcore.DebugLevel) {
			logx.L().Debugw("task dropped packet due to full worker pool", "task", t.Name, "pool_size", t.PoolSize, "error", err)
		}
		return
	}
}

// StopGraceful 关闭接收入口并等待在途任务完成，再释放资源池。
func (t *Task) StopGraceful() {
	t.accepting.Store(false)
	t.inflight.Wait()
	if t.pool != nil {
		t.pool.Release()
		t.pool = nil
	}
	if t.stats != nil {
		t.stats.Close()
		t.stats = nil
	}
}

// processAndSend 依次执行 pipeline，再将结果发送到所有 sender。
func (t *Task) processAndSend(ctx context.Context, pkt *packet.Packet) {
	for _, pl := range t.Pipelines {
		if !pl.Process(pkt) {
			return
		}
	}
	for _, s := range t.Senders {
		if t.stats != nil {
			t.stats.AddBytes(len(pkt.Payload))
		}
		if err := s.Send(ctx, pkt); err != nil && logx.Enabled(zapcore.WarnLevel) {
			logx.L().Warnw("sender send failed", "task", t.Name, "error", err)
		}
	}
}
