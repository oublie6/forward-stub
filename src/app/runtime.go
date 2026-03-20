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
	// store 持有所有运行时组件快照，是启动/热重载/停机的核心状态容器。
	store *runtime.Store

	// systemMu 保护 systemCfg / hasSystem，确保热重载校验基线时读写一致。
	systemMu sync.RWMutex
	// systemCfg 是冷启动时固化的系统配置基线；业务热重载时必须保持不变。
	systemCfg config.SystemConfig
	// hasSystem 标识是否已经完成基线写入，防止在未初始化前误做稳定性比较。
	hasSystem bool
}

// NewRuntime 创建应用层 Runtime 门面。
// 它把底层 runtime.Store 与启动/重载所需的 system 配置稳定性检查组合在一起。
func NewRuntime() *Runtime {
	return &Runtime{store: runtime.NewStore()}
}

// UpdateCache 把一份已校验的业务配置应用到运行时。
// 真正的构建、复用、切换逻辑都在 runtime.UpdateCache 中完成。
func (r *Runtime) UpdateCache(ctx context.Context, cfg config.Config) error {
	return runtime.UpdateCache(ctx, r.store, cfg)
}

// SeedSystemConfig 在服务启动时记录系统配置基线。
func (r *Runtime) SeedSystemConfig(cfg config.SystemConfig) error {
	r.systemMu.Lock()
	defer r.systemMu.Unlock()
	if r.hasSystem {
		if !reflect.DeepEqual(r.systemCfg, cfg) {
			return fmt.Errorf("系统配置已变化，需要重启服务")
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
		return fmt.Errorf("系统配置基线尚未初始化")
	}
	if !reflect.DeepEqual(r.systemCfg, cfg) {
		return fmt.Errorf("系统配置已变化，需要重启服务后生效")
	}
	return nil
}

// Stop 停止 runtime 持有的全部组件。
// bootstrap 停机流程会通过它统一下发 stop 信号。
func (r *Runtime) Stop(ctx context.Context) error {
	return r.store.StopAll(ctx)
}
