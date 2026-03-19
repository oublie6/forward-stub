package bootstrap

import (
	"context"
	"runtime"
	"time"

	"forward-stub/src/config"
)

type stopFunc func()

func startGCStatsLogger(parent context.Context, lg *stageLogger, enabled bool, interval time.Duration) stopFunc {
	if !enabled {
		lg.Info("gc_stats", "GC周期日志未启用")
		return func() {}
	}
	if interval <= 0 {
		fallback, _ := time.ParseDuration(config.DefaultGCStatsLogInterval)
		lg.Warn("gc_stats", "GC周期日志间隔无效，已回退到默认值", "配置值", interval.String(), "默认值", config.DefaultGCStatsLogInterval)
		interval = fallback
	}

	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	ticker := time.NewTicker(interval)
	go func() {
		defer close(done)
		defer ticker.Stop()

		lg.Info("gc_stats", "GC周期日志任务已启动", "日志间隔", interval.String())
		logGCStats(lg)
		for {
			select {
			case <-ctx.Done():
				lg.Info("gc_stats", "GC周期日志任务已停止")
				return
			case <-ticker.C:
				logGCStats(lg)
			}
		}
	}()

	return func() {
		cancel()
		<-done
	}
}

func logGCStats(lg *stageLogger) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	var lastPause uint64
	if ms.NumGC > 0 {
		lastPause = ms.PauseNs[(ms.NumGC-1)%uint32(len(ms.PauseNs))]
	}
	now := time.Now()
	lg.Info("gc_stats", "GC周期信息",
		"采样时间", now.Format(time.RFC3339Nano),
		"goroutine数量", runtime.NumGoroutine(),
		"heap_alloc", ms.HeapAlloc,
		"heap_inuse", ms.HeapInuse,
		"heap_sys", ms.HeapSys,
		"stack_inuse", ms.StackInuse,
		"next_gc", ms.NextGC,
		"gc次数", ms.NumGC,
		"最近一次GC时间", time.Unix(0, int64(ms.LastGC)).Format(time.RFC3339Nano),
		"最近一次GC暂停纳秒", lastPause,
		"gc_cpu_fraction", ms.GCCPUFraction,
	)
}
