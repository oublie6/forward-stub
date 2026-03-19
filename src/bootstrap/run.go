package bootstrap

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	goruntime "runtime"
	"sync"
	"time"

	"forward-stub/src/app"
	"forward-stub/src/config"
	"forward-stub/src/control"
	"forward-stub/src/logx"
)

const shutdownTimeout = 10 * time.Second

// Run 负责解析参数、加载配置并驱动运行时生命周期。
func Run(args []string) int {
	bootLog := newStageLogger()
	bootLog.Info("process_start", "forward-stub process starting",
		"pid", os.Getpid(),
		"go_version", goruntime.Version(),
		"gomaxprocs", goruntime.GOMAXPROCS(0),
		"host_cpu_cores", goruntime.NumCPU(),
	)

	fs := flag.NewFlagSet("forward-stub", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	legacyPath := fs.String("config", "", "legacy config json path (same file for system and business)")
	systemPath := fs.String("system-config", "", "system config json path")
	businessPath := fs.String("business-config", "", "business config json path")
	if err := fs.Parse(args); err != nil {
		bootLog.Error("args_parse", "parse cli args failed", err)
		return 2
	}
	bootLog.Info("args_parse", "cli args parsed", "legacy_mode", *legacyPath != "", "arg_count", len(args))

	sysPath, bizPath, err := config.ResolveConfigPaths(*legacyPath, *systemPath, *businessPath)
	if err != nil {
		bootLog.Error("config_path_resolve", "resolve config paths failed", err)
		return 1
	}
	bootLog.Info("config_path_resolve", "config paths resolved", "system_config", sysPath, "business_config", bizPath)

	bootLog.Info("config_load", "loading config files")
	sysCfg, _, cfg, err := loadConfigPair(context.Background(), sysPath, bizPath)
	if err != nil {
		bootLog.Error("config_load", "load config failed", err, "system_config", sysPath, "business_config", bizPath)
		return 1
	}
	bootLog.Info("config_load", "config files loaded")
	bootLog.Info("config_validate", "config validated", "config_version", cfg.Version)

	trafficStatsInterval, err := time.ParseDuration(cfg.Logging.TrafficStatsInterval)
	if err != nil {
		bootLog.Error("logger_init", "parse traffic stats interval failed", err, "value", cfg.Logging.TrafficStatsInterval)
		return 1
	}
	if err := logx.Init(logx.Options{
		Level:                   cfg.Logging.Level,
		File:                    cfg.Logging.File,
		MaxSizeMB:               cfg.Logging.MaxSizeMB,
		MaxBackups:              cfg.Logging.MaxBackups,
		MaxAgeDays:              cfg.Logging.MaxAgeDays,
		Compress:                config.BoolValue(cfg.Logging.Compress),
		TrafficStatsInterval:    trafficStatsInterval,
		TrafficStatsSampleEvery: cfg.Logging.TrafficStatsSampleEvery,
	}); err != nil {
		bootLog.Error("logger_init", "init logger failed", err)
		return 1
	}
	defer func() { _ = logx.Sync() }()
	bootLog.Attach(logx.L())
	bootLog.Info("logger_init", "logger initialized", "level", cfg.Logging.Level, "file", cfg.Logging.File)
	logConfigSummary(bootLog, sysPath, bizPath, cfg)

	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()

	gcLogInterval, _ := time.ParseDuration(cfg.Logging.GCStatsLogInterval)
	stopGCStats := startGCStatsLogger(runCtx, bootLog, config.BoolValue(cfg.Logging.GCStatsLogEnabled), gcLogInterval)
	var stopGCOnce sync.Once
	stopGC := func() { stopGCOnce.Do(stopGCStats) }
	defer stopGC()

	bootLog.Info("pprof_init", "initializing pprof server", "port", cfg.Control.PprofPort)
	pprofSrv := startPprofServer(logx.L(), cfg.Control.PprofPort)
	var stopPprofOnce sync.Once
	stopPprof := func() { stopPprofOnce.Do(func() { shutdownPprofServer(pprofSrv, logx.L()) }) }
	defer stopPprof()

	rt := app.NewRuntime()
	bootLog.Info("runtime_init", "seeding system config baseline")
	if err := rt.SeedSystemConfig(sysCfg); err != nil {
		bootLog.Error("runtime_init", "seed system config failed", err)
		return 1
	}
	bootLog.Info("runtime_init", "system config baseline seeded")

	bootLog.Info("runtime_init", "initializing runtime cache", "config_version", cfg.Version)
	if err := rt.UpdateCache(runCtx, cfg); err != nil {
		bootLog.Error("runtime_init", "runtime cache initialization failed", err)
		shutdownRuntime(rt, bootLog)
		return 1
	}
	bootLog.Info("runtime_init", "runtime cache initialized", "config_version", cfg.Version)

	initialFingerprint, err := readConfigFingerprint(bizPath)
	if err != nil {
		bootLog.Error("config_watch", "read initial business config fingerprint failed", err, "config", bizPath)
		shutdownRuntime(rt, bootLog)
		return 1
	}

	configChangeCh := make(chan struct{}, 1)
	watchCtx, cancelWatch := context.WithCancel(runCtx)
	defer cancelWatch()
	bootLog.Info("config_watch", "starting config file watcher", "config", bizPath, "interval", cfg.Control.ConfigWatchInterval)
	go watchConfigFile(watchCtx, bizPath, initialFingerprint, cfg.Control.ConfigWatchInterval, configChangeCh, bootLog)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, supportedSignals()...)
	defer signal.Stop(sigCh)
	bootLog.Info("signal_init", "signal handlers registered")
	bootLog.Info("service_start", "forward-stub service started", "system_config", sysPath, "business_config", bizPath, "config_version", cfg.Version)

	for {
		select {
		case s := <-sigCh:
			if isReloadSignal(s) {
				bootLog.Info("reload_begin", "received reload signal", "signal", s.String())
				if next, ok := reloadAndApplyBusinessConfig(runCtx, rt, sysPath, bizPath, bootLog, "signal", s.String()); ok {
					bootLog.Info("reload_done", "reload business config succeeded", "signal", s.String(), "config_version", next.Version)
				}
				continue
			}

			if isStopSignal(s) {
				bootLog.Info("shutdown_begin", "graceful shutdown starting", "signal", s.String())
				cancelWatch()
				cancelRun()
				stopGC()
				shutdownRuntime(rt, bootLog)
				stopPprof()
				bootLog.Info("shutdown_done", "graceful shutdown completed", "signal", s.String())
				return 0
			}

			bootLog.Warn("signal_unhandled", "received unsupported signal", "signal", s.String())
		case <-configChangeCh:
			bootLog.Info("reload_begin", "detected business config file change", "config", bizPath)
			next, ok := reloadAndApplyBusinessConfig(runCtx, rt, sysPath, bizPath, bootLog, "source", "file-watch")
			if !ok {
				continue
			}
			bootLog.Info("reload_done", "reload business config succeeded", "source", "file-watch", "config_version", next.Version)
		}
	}
}

type pprofResponseRecorder struct {
	http.ResponseWriter
	status int
	size   int
}

func (r *pprofResponseRecorder) WriteHeader(statusCode int) {
	r.status = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *pprofResponseRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.size += n
	return n, err
}

func withPprofRequestLog(next http.Handler, lg interface{ Infow(string, ...interface{}) }) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &pprofResponseRecorder{ResponseWriter: w}
		next.ServeHTTP(rw, r)
		status := rw.status
		if status == 0 {
			status = http.StatusOK
		}
		lg.Infow("pprof request completed",
			"method", r.Method,
			"path", r.URL.Path,
			"query", r.URL.RawQuery,
			"remote_addr", r.RemoteAddr,
			"user_agent", r.UserAgent(),
			"status", status,
			"response_bytes", rw.size,
			"duration", time.Since(start).String(),
		)
	})
}

func startPprofServer(lg interface {
	Infow(string, ...interface{})
	Warnw(string, ...interface{})
}, port int) *http.Server {
	if port <= 0 {
		lg.Infow("pprof http server disabled", "port", port)
		return nil
	}
	addr := fmt.Sprintf(":%d", port)
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	srv := &http.Server{Addr: addr, Handler: withPprofRequestLog(mux, lg)}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			lg.Warnw("pprof http server exited unexpectedly", "addr", addr, "error", err)
		}
	}()
	lg.Infow("pprof http server enabled", "addr", addr)
	return srv
}

func shutdownPprofServer(srv *http.Server, lg interface {
	Infow(string, ...interface{})
	Warnw(string, ...interface{})
}) {
	if srv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		lg.Warnw("shutdown pprof http server failed", "addr", srv.Addr, "error", err)
		return
	}
	lg.Infow("pprof http server stopped", "addr", srv.Addr)
}

func watchConfigFile(ctx context.Context, path, initialFingerprint, watchInterval string, notifyCh chan<- struct{}, bootLog *stageLogger) {
	currentFingerprint := initialFingerprint
	interval, err := time.ParseDuration(watchInterval)
	if err != nil || interval <= 0 {
		bootLog.Warn("config_watch", "invalid config watch interval, fallback to default", "value", watchInterval, "default", config.DefaultConfigWatchInterval, "error", err)
		interval, _ = time.ParseDuration(config.DefaultConfigWatchInterval)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	bootLog.Info("config_watch", "config file watcher started", "config", path, "interval", interval.String())
	defer bootLog.Info("config_watch", "config file watcher stopped", "config", path)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			nextFingerprint, err := readConfigFingerprint(path)
			if err != nil {
				bootLog.Warn("config_watch", "watch config file failed", "config", path, "error", err)
				continue
			}
			if nextFingerprint == currentFingerprint {
				continue
			}
			currentFingerprint = nextFingerprint
			select {
			case notifyCh <- struct{}{}:
			default:
			}
		}
	}
}

func reloadAndApplyBusinessConfig(ctx context.Context, rt *app.Runtime, systemPath, businessPath string, bootLog *stageLogger, sourceKey, sourceValue string) (config.Config, bool) {
	systemCfg, _, next, err := loadConfigPair(ctx, systemPath, businessPath)
	if err != nil {
		bootLog.Error("reload_config", "reload config failed", err, sourceKey, sourceValue)
		return config.Config{}, false
	}
	if err := rt.CheckSystemConfigStable(systemCfg); err != nil {
		bootLog.Error("reload_config", "reject business reload due to system config change", err, sourceKey, sourceValue)
		return config.Config{}, false
	}
	if err := rt.UpdateCache(ctx, next); err != nil {
		bootLog.Error("reload_config", "apply reloaded config failed", err, sourceKey, sourceValue)
		return config.Config{}, false
	}
	bootLog.Info("reload_config", "reloaded config applied", sourceKey, sourceValue, "config_version", next.Version)
	return next, true
}

func loadConfigPair(ctx context.Context, systemPath, businessPath string) (config.SystemConfig, config.BusinessConfig, config.Config, error) {
	sys, biz, cfg, err := config.LoadLocalPair(systemPath, businessPath)
	if err != nil {
		return config.SystemConfig{}, config.BusinessConfig{}, config.Config{}, err
	}
	if cfg.Control.API != "" {
		cli := control.NewConfigAPIClient(cfg.Control.API, cfg.Control.TimeoutSec)
		biz, err = cli.FetchBusinessConfig(ctx)
		if err != nil {
			return config.SystemConfig{}, config.BusinessConfig{}, config.Config{}, fmt.Errorf("fetch business config from api error: %w", err)
		}
		cfg = sys.Merge(biz)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return config.SystemConfig{}, config.BusinessConfig{}, config.Config{}, fmt.Errorf("config validate error: %w", err)
	}
	return sys, biz, cfg, nil
}

func shutdownRuntime(rt *app.Runtime, bootLog *stageLogger) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := rt.Stop(ctx); err != nil {
		bootLog.Error("shutdown_runtime", "runtime stop failed", err, "timeout", shutdownTimeout.String())
		return
	}
	bootLog.Info("shutdown_runtime", "runtime stopped", "timeout", shutdownTimeout.String())
}
