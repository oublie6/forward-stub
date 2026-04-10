package bootstrap

import (
	"context"
	"fmt"

	"forward-stub/src/app"
	"forward-stub/src/logx"
	"forward-stub/src/schedule"
)

// initScheduleManager 创建并注册 schedule manager。
//
// 当前阶段只搭调度框架骨架，不接真实业务任务。
func initScheduleManager(rt *app.Runtime) (*schedule.Manager, error) {
	if rt == nil {
		return nil, fmt.Errorf("schedule manager 初始化失败: runtime 不能为空")
	}
	mgr := schedule.NewManager(logx.L())
	if err := registerSchedules(mgr, rt); err != nil {
		return nil, err
	}
	return mgr, nil
}

// registerSchedules 统一收口服务内定时任务注册入口。
//
// 设计约束：
// 1. 后续链监发送、全局单例巡检等任务，应统一挂到 leader_only group；
// 2. 每实例都需要执行的本地维护任务，应统一挂到 local group；
// 3. 避免在业务代码里继续散落 ticker / goroutine，而是统一通过 schedule manager 承载。
//
// 当前阶段先保留注册挂点，不实际注册业务任务。
func registerSchedules(mgr *schedule.Manager, rt *app.Runtime) error {
	if mgr == nil {
		return fmt.Errorf("schedule 注册失败: manager 不能为空")
	}
	if rt == nil {
		return fmt.Errorf("schedule 注册失败: runtime 不能为空")
	}
	return mgr.Register(schedule.RegistrarFunc(func(m *schedule.Manager) error {
		_, _ = rt, m
		return nil
	}))
}

// shutdownScheduleManager 在固定超时窗口内停止 schedule manager。
func shutdownScheduleManager(mgr *schedule.Manager, bootLog *stageLogger) {
	if mgr == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := mgr.Stop(ctx); err != nil {
		bootLog.Error("shutdown_schedule", "定时任务管理器停止失败", err, "超时时间", shutdownTimeout.String())
		return
	}
	bootLog.Info("shutdown_schedule", "定时任务管理器已停止", "超时时间", shutdownTimeout.String())
}
