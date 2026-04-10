package schedule

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// IntervalJobOption 用于配置 IntervalJob。
type IntervalJobOption func(*IntervalJob)

// WithImmediateStart 表示 job 启动后立即执行一次。
func WithImmediateStart() IntervalJobOption {
	return func(job *IntervalJob) {
		job.immediateStart = true
	}
}

// WithAllowOverlap 表示允许多轮执行并发重入。
func WithAllowOverlap() IntervalJobOption {
	return func(job *IntervalJob) {
		job.allowOverlap = true
	}
}

// IntervalJob 是一个基于 time.Ticker 的固定周期任务。
//
// 默认行为：
// 1. 启动后按 interval 周期执行；
// 2. 默认不允许重入，上一轮未结束时当前轮直接跳过；
// 3. 在 ctx.Done() 后停止接收新的 tick，并等待内部执行 goroutine 回收完成。
type IntervalJob struct {
	name     string
	interval time.Duration
	run      TaskFunc

	immediateStart bool
	allowOverlap   bool
	running        atomic.Bool
}

// NewIntervalJob 创建一个新的 interval job。
func NewIntervalJob(name string, interval time.Duration, run TaskFunc, opts ...IntervalJobOption) (*IntervalJob, error) {
	if name == "" {
		return nil, fmt.Errorf("schedule interval job 名称不能为空")
	}
	if interval <= 0 {
		return nil, fmt.Errorf("schedule interval job %s 周期必须大于 0", name)
	}
	if run == nil {
		return nil, fmt.Errorf("schedule interval job %s 执行函数不能为空", name)
	}

	job := &IntervalJob{
		name:     name,
		interval: interval,
		run:      run,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(job)
		}
	}
	return job, nil
}

// Name 返回任务名称。
func (j *IntervalJob) Name() string {
	return j.name
}

// Start 启动 interval job 主循环，并在 ctx.Done() 后等待内部执行退出。
func (j *IntervalJob) Start(ctx context.Context, logger *zap.SugaredLogger) {
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}

	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()

	var wg sync.WaitGroup
	startRun := func(trigger string) {
		if !j.allowOverlap && !j.running.CompareAndSwap(false, true) {
			logger.Warnw("schedule interval job 上一轮尚未结束，当前轮已跳过",
				"job", j.name,
				"触发方式", trigger,
				"周期", j.interval.String(),
			)
			return
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			if !j.allowOverlap {
				defer j.running.Store(false)
			}
			defer func() {
				if rec := recover(); rec != nil {
					logger.Errorw("schedule interval job 执行 panic",
						"job", j.name,
						"panic", rec,
					)
				}
			}()

			if err := j.run(ctx); err != nil {
				logger.Errorw("schedule interval job 执行失败",
					"job", j.name,
					"错误", err,
				)
			}
		}()
	}

	if j.immediateStart {
		startRun("immediate")
	}

	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case <-ticker.C:
			startRun("ticker")
		}
	}
}
