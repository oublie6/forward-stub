package schedule

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"
)

// Mode 描述任务分组的运行模式。
type Mode string

const (
	// ModeLocal 表示每个实例都运行该组任务。
	ModeLocal Mode = "local"
	// ModeLeaderOnly 表示只有 leader 实例运行该组任务。
	ModeLeaderOnly Mode = "leader_only"
)

// Elector 用于判断当前实例在当前轮次是否具备 leader 身份。
//
// 约定：
// 1. 返回 nil 表示当前实例是 leader；
// 2. 返回 error 表示当前实例不是 leader，或本轮选主状态不可用。
type Elector interface {
	IsLeader(context.Context) error
}

// Registrar 负责向 Manager 注册一批 group / job。
//
// 业务层后续应统一通过 registrar 收口任务注册，避免散落在各处直接起 ticker。
type Registrar interface {
	Register(*Manager) error
}

// RegistrarFunc 允许直接使用函数作为 Registrar。
type RegistrarFunc func(*Manager) error

// Register 调用具体注册函数。
func (f RegistrarFunc) Register(mgr *Manager) error {
	return f(mgr)
}

// Manager 负责统一承载 schedule 模块生命周期。
//
// 职责包括：
// 1. 接收 registrar；
// 2. 由 registrar 创建并装配 group；
// 3. 统一启动全部 group；
// 4. 在停机阶段统一停止全部 group。
//
// 并发语义：
// 1. Start 成功后不允许再注册 registrar 或新增 group；
// 2. Stop 会等待全部 group 退出完成；
// 3. 同一个 Manager 只允许成功启动一次。
type Manager struct {
	mu sync.Mutex

	logger *zap.SugaredLogger

	registrars []Registrar
	groups     []*Group

	started bool
	stopped bool

	runCtx    context.Context
	cancelRun context.CancelFunc
	wg        sync.WaitGroup
}

// NewManager 创建一个新的调度管理器。
func NewManager(logger *zap.SugaredLogger) *Manager {
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}
	return &Manager{logger: logger}
}

// Logger 返回 manager 持有的日志器。
func (m *Manager) Logger() *zap.SugaredLogger {
	return m.logger
}

// Register 注册一个 registrar。
func (m *Manager) Register(reg Registrar) error {
	if reg == nil {
		return fmt.Errorf("schedule registrar 不能为空")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return fmt.Errorf("schedule manager 已启动，不能再注册 registrar")
	}
	m.registrars = append(m.registrars, reg)
	return nil
}

// NewGroup 创建一个新的任务分组。
func (m *Manager) NewGroup(name string, mode Mode, opts ...GroupOption) (*Group, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return nil, fmt.Errorf("schedule manager 已启动，不能再新增 group")
	}

	group, err := newGroup(name, mode, m.logger, opts...)
	if err != nil {
		return nil, err
	}
	m.groups = append(m.groups, group)
	return group, nil
}

// Start 触发 registrar 注册，并统一启动全部 group。
func (m *Manager) Start(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("schedule manager 启动失败: context 不能为空")
	}

	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return fmt.Errorf("schedule manager 已启动")
	}

	registrars := append([]Registrar(nil), m.registrars...)
	m.mu.Unlock()

	for _, reg := range registrars {
		if err := reg.Register(m); err != nil {
			return fmt.Errorf("schedule registrar 注册失败: %w", err)
		}
	}

	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return fmt.Errorf("schedule manager 已启动")
	}
	runCtx, cancel := context.WithCancel(ctx)
	m.runCtx = runCtx
	m.cancelRun = cancel
	groups := append([]*Group(nil), m.groups...)
	m.started = true
	m.mu.Unlock()

	for _, group := range groups {
		if err := group.Start(runCtx); err != nil {
			cancel()
			_ = m.waitStop(context.Background())
			return fmt.Errorf("schedule group 启动失败: %w", err)
		}
		m.wg.Add(1)
		go func(g *Group) {
			defer m.wg.Done()
			g.Wait()
		}(group)
	}

	m.logger.Infow("schedule manager 已启动", "group数量", len(groups))
	return nil
}

// Stop 统一停止全部 group，并等待它们退出。
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	if !m.started {
		m.stopped = true
		m.mu.Unlock()
		return nil
	}
	if m.stopped {
		m.mu.Unlock()
		return m.waitStop(ctx)
	}
	m.stopped = true
	cancel := m.cancelRun
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	if err := m.waitStop(ctx); err != nil {
		return err
	}
	m.logger.Infow("schedule manager 已停止")
	return nil
}

func (m *Manager) waitStop(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	if ctx == nil {
		<-done
		return nil
	}

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("等待 schedule manager 停止失败: %w", ctx.Err())
	}
}
