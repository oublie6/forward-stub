package app

import (
	"context"

	"forword-stub/src/config"
	"forword-stub/src/runtime"
)

type Runtime struct {
	store *runtime.Store
}

func NewRuntime() *Runtime {
	return &Runtime{store: runtime.NewStore()}
}

func (r *Runtime) UpdateCache(ctx context.Context, cfg config.Config) error {
	return runtime.UpdateCache(ctx, r.store, cfg)
}

func (r *Runtime) Stop(ctx context.Context) error {
	return r.store.StopAll(ctx)
}
