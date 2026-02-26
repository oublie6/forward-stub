// runtime.go 暴露应用层可直接调用的运行时启动与更新接口。
package app

import (
	"context"

	"forward-stub/src/config"
	"forward-stub/src/runtime"
)

type Runtime struct {
	store *runtime.Store
}

// NewRuntime 负责该函数对应的核心逻辑，详见实现细节。
func NewRuntime() *Runtime {
	return &Runtime{store: runtime.NewStore()}
}

// UpdateCache 负责该函数对应的核心逻辑，详见实现细节。
func (r *Runtime) UpdateCache(ctx context.Context, cfg config.Config) error {
	return runtime.UpdateCache(ctx, r.store, cfg)
}

// Stop 负责该函数对应的核心逻辑，详见实现细节。
func (r *Runtime) Stop(ctx context.Context) error {
	return r.store.StopAll(ctx)
}
