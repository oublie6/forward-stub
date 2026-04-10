package schedule

import (
	"context"

	"go.uber.org/zap"
)

// TaskFunc 表示一次具体的任务执行函数。
//
// 约定：
// 1. ctx 用于感知停机与取消；
// 2. 返回 error 表示本轮执行失败，由调度器负责记录日志。
type TaskFunc func(context.Context) error

// Job 描述一个可由 Group 托管的调度任务。
//
// 生命周期约定：
// 1. Start 会在 group 启动后由调度框架调用；
// 2. Start 应在 ctx.Done() 后尽快退出；
// 3. 每个 job 自己负责等待其内部派生的 goroutine 回收完成。
type Job interface {
	Name() string
	Start(context.Context, *zap.SugaredLogger)
}
