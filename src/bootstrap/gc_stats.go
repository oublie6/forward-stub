package bootstrap

import (
	"context"
	"runtime"
	"time"
)

type stopFunc func()

func startGCStatsLogger(parent context.Context, lg *stageLogger, enabled bool, interval time.Duration) stopFunc {
	if !enabled {
		lg.Info("gc_stats_disabled", "gc stats logger disabled")
		return func() {}
	}

	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	ticker := time.NewTicker(interval)
	go func() {
		defer close(done)
		defer ticker.Stop()

		lg.Info("gc_stats_started", "gc stats logger started", "interval", interval.String())
		logGCStats(lg)
		for {
			select {
			case <-ctx.Done():
				lg.Info("gc_stats_stopped", "gc stats logger stopped")
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
	lg.Info("gc_stats_tick", "runtime gc stats",
		"goroutines", runtime.NumGoroutine(),
		"heap_alloc", ms.HeapAlloc,
		"heap_inuse", ms.HeapInuse,
		"heap_sys", ms.HeapSys,
		"next_gc", ms.NextGC,
		"num_gc", ms.NumGC,
		"last_gc", time.Unix(0, int64(ms.LastGC)).Format(time.RFC3339Nano),
		"last_gc_pause_ns", lastPause,
	)
}
