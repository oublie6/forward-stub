// runtime.go 暴露应用层可直接调用的运行时启动与更新接口。
package app

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"forward-stub/src/config"
	"forward-stub/src/runtime"
)

type Runtime struct {
	store *runtime.Store

	systemMu  sync.RWMutex
	systemCfg config.SystemConfig
	hasSystem bool
}

// NewRuntime 负责该函数对应的核心逻辑，详见实现细节。
func NewRuntime() *Runtime {
	return &Runtime{store: runtime.NewStore()}
}

// UpdateCache 负责该函数对应的核心逻辑，详见实现细节。
func (r *Runtime) UpdateCache(ctx context.Context, cfg config.Config) error {
	return runtime.UpdateCache(ctx, r.store, cfg)
}

// SeedSystemConfig 在服务启动时记录系统配置基线。
func (r *Runtime) SeedSystemConfig(cfg config.SystemConfig) error {
	r.systemMu.Lock()
	defer r.systemMu.Unlock()
	if r.hasSystem {
		if !reflect.DeepEqual(r.systemCfg, cfg) {
			return fmt.Errorf("system config changed and requires restart")
		}
		return nil
	}
	r.systemCfg = cfg
	r.hasSystem = true
	return nil
}

// CheckSystemConfigStable 校验热重载期间系统配置是否被修改。
func (r *Runtime) CheckSystemConfigStable(cfg config.SystemConfig) error {
	r.systemMu.RLock()
	defer r.systemMu.RUnlock()
	if !r.hasSystem {
		return fmt.Errorf("system config baseline not initialized")
	}
	if !reflect.DeepEqual(r.systemCfg, cfg) {
		return fmt.Errorf("system config changed, restart service to take effect")
	}
	return nil
}

// Stop 负责该函数对应的核心逻辑，详见实现细节。
func (r *Runtime) Stop(ctx context.Context) error {
	return r.store.StopAll(ctx)
}
