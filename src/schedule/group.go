package schedule

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

const defaultLeaderCheckInterval = 3 * time.Second

// GroupOption 用于配置 Group 的运行参数。
type GroupOption func(*Group)

// WithElector 为 leader_only 分组设置选主判断器。
func WithElector(elector Elector) GroupOption {
	return func(g *Group) {
		g.elector = elector
	}
}

// WithLeaderCheckInterval 设置 leader_only 分组的 leader 检查周期。
func WithLeaderCheckInterval(interval time.Duration) GroupOption {
	return func(g *Group) {
		g.leaderCheckInterval = interval
	}
}

// Group 表示一组具备相同运行边界的定时任务。
//
// 生命周期语义：
// 1. Add / Every 只能在 Start 前调用；
// 2. Start 后会由 group 自己托管内部 goroutine；
// 3. Wait 会等待 group 及其托管 job 全部退出。
//
// 并发语义：
// 1. local 模式下，group 启动后直接拉起全部 job；
// 2. leader_only 模式下，group 会周期性检查 leader 状态，并在 leader 身份变化时启停整组 job；
// 3. 失去 leader 后会先取消当前组内 job，再等待其全部退出。
type Group struct {
	mu sync.Mutex

	name string
	mode Mode

	logger *zap.SugaredLogger

	elector             Elector
	leaderCheckInterval time.Duration

	jobs []Job

	started bool
	startWg sync.WaitGroup
}

func newGroup(name string, mode Mode, logger *zap.SugaredLogger, opts ...GroupOption) (*Group, error) {
	if name == "" {
		return nil, fmt.Errorf("schedule group 名称不能为空")
	}
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}

	group := &Group{
		name:                name,
		mode:                mode,
		logger:              logger,
		leaderCheckInterval: defaultLeaderCheckInterval,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(group)
		}
	}
	if group.mode != ModeLocal && group.mode != ModeLeaderOnly {
		return nil, fmt.Errorf("schedule group %s 模式不支持: %s", name, mode)
	}
	if group.leaderCheckInterval <= 0 {
		group.leaderCheckInterval = defaultLeaderCheckInterval
	}
	return group, nil
}

// Name 返回分组名称。
func (g *Group) Name() string {
	return g.name
}

// Add 向分组内追加一个 job。
func (g *Group) Add(job Job) error {
	if job == nil {
		return fmt.Errorf("schedule group %s 添加任务失败: job 不能为空", g.name)
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.started {
		return fmt.Errorf("schedule group %s 已启动，不能再添加任务", g.name)
	}
	g.jobs = append(g.jobs, job)
	return nil
}

// Every 创建一个 interval job 并加入当前 group。
func (g *Group) Every(name string, interval time.Duration, run TaskFunc, opts ...IntervalJobOption) error {
	job, err := NewIntervalJob(name, interval, run, opts...)
	if err != nil {
		return err
	}
	return g.Add(job)
}

// Start 启动分组。
func (g *Group) Start(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("schedule group %s 启动失败: context 不能为空", g.name)
	}

	g.mu.Lock()
	if g.started {
		g.mu.Unlock()
		return fmt.Errorf("schedule group %s 已启动", g.name)
	}
	g.started = true
	g.startWg.Add(1)
	g.mu.Unlock()

	switch g.mode {
	case ModeLocal:
		go g.runLocal(ctx)
	case ModeLeaderOnly:
		go g.runLeaderOnly(ctx)
	default:
		g.startWg.Done()
		return fmt.Errorf("schedule group %s 模式不支持: %s", g.name, g.mode)
	}
	return nil
}

// Wait 等待分组退出。
func (g *Group) Wait() {
	g.startWg.Wait()
}

func (g *Group) runLocal(ctx context.Context) {
	defer g.startWg.Done()
	runJobs(ctx, g.jobs, g.logger, g.name, g.mode)
	<-ctx.Done()
}

func (g *Group) runLeaderOnly(ctx context.Context) {
	defer g.startWg.Done()

	if g.elector == nil {
		g.logger.Warnw("leader_only 分组未配置 elector，当前不会启动任何任务",
			"group", g.name,
			"mode", g.mode,
		)
		<-ctx.Done()
		return
	}

	ticker := time.NewTicker(g.leaderCheckInterval)
	defer ticker.Stop()

	var (
		jobsCancel context.CancelFunc
		jobsDone   chan struct{}
		running    bool
	)

	stopJobs := func(reason string, err error) {
		if !running {
			return
		}
		if err != nil {
			g.logger.Warnw("leader_only 分组停止任务",
				"group", g.name,
				"原因", reason,
				"错误", err,
			)
		} else {
			g.logger.Infow("leader_only 分组停止任务", "group", g.name, "原因", reason)
		}
		if jobsCancel != nil {
			jobsCancel()
		}
		if jobsDone != nil {
			<-jobsDone
		}
		jobsCancel = nil
		jobsDone = nil
		running = false
	}

	startJobs := func() {
		if running {
			return
		}
		jobCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			runJobs(jobCtx, g.jobs, g.logger, g.name, g.mode)
			<-jobCtx.Done()
		}()
		jobsCancel = cancel
		jobsDone = done
		running = true
		g.logger.Infow("leader_only 分组开始运行任务", "group", g.name, "job数量", len(g.jobs))
	}

	checkLeader := func() {
		err := g.elector.IsLeader(ctx)
		if err == nil {
			startJobs()
			return
		}
		if running {
			stopJobs("失去 leader 身份", err)
			return
		}
		g.logger.Warnw("leader_only 分组当前不是 leader，本轮不启动任务",
			"group", g.name,
			"错误", err,
		)
	}
	checkLeader()

	for {
		select {
		case <-ctx.Done():
			stopJobs("group 上下文结束", nil)
			return
		case <-ticker.C:
			checkLeader()
		}
	}
}

func runJobs(ctx context.Context, jobs []Job, logger *zap.SugaredLogger, groupName string, mode Mode) {
	var wg sync.WaitGroup
	for _, job := range jobs {
		wg.Add(1)
		go func(j Job) {
			defer wg.Done()
			logger.Infow("schedule job 启动", "group", groupName, "mode", mode, "job", j.Name())
			j.Start(ctx, logger)
			logger.Infow("schedule job 退出", "group", groupName, "mode", mode, "job", j.Name())
		}(job)
	}
	wg.Wait()
}
