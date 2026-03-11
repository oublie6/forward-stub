package bootstrap

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"time"

	"forward-stub/src/app"
	"forward-stub/src/config"
	"forward-stub/src/control"
	"forward-stub/src/logx"
)

// Run 负责解析参数、加载配置并驱动运行时生命周期。
func Run(version string, args []string) int {
	fs := flag.NewFlagSet("forward-stub", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	legacyPath := fs.String("config", "", "legacy config json path (same file for system and business)")
	systemPath := fs.String("system-config", "", "system config json path")
	businessPath := fs.String("business-config", "", "business config json path")
	showVersion := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *showVersion {
		_, _ = os.Stdout.WriteString(version + "\n")
		return 0
	}

	sysPath, bizPath, err := config.ResolveConfigPaths(*legacyPath, *systemPath, *businessPath)
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		return 1
	}

	sysCfg, _, cfg, err := loadConfigPair(context.Background(), sysPath, bizPath)
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		return 1
	}

	trafficStatsInterval, err := time.ParseDuration(cfg.Logging.TrafficStatsInterval)
	if err != nil {
		_, _ = os.Stderr.WriteString("invalid traffic_stats_interval: " + err.Error() + "\n")
		return 1
	}
	if err := logx.Init(logx.Options{
		Level:                   cfg.Logging.Level,
		File:                    cfg.Logging.File,
		MaxSizeMB:               cfg.Logging.MaxSizeMB,
		MaxBackups:              cfg.Logging.MaxBackups,
		MaxAgeDays:              cfg.Logging.MaxAgeDays,
		Compress:                *cfg.Logging.Compress,
		TrafficStatsInterval:    trafficStatsInterval,
		TrafficStatsSampleEvery: cfg.Logging.TrafficStatsSampleEvery,
	}); err != nil {
		_, _ = os.Stderr.WriteString("init logger error: " + err.Error() + "\n")
		return 1
	}
	defer func() { _ = logx.Sync() }()
	lg := logx.L()

	pprofSrv := startPprofServer(lg, cfg.Control.PprofPort)

	rt := app.NewRuntime()
	if err := rt.UpdateCache(context.Background(), cfg); err != nil {
		lg.Errorf("UpdateCache error: %v", err)
		return 1
	}
	if err := rt.SeedSystemConfig(sysCfg); err != nil {
		lg.Errorf("seed system config error: %v", err)
		return 1
	}

	initialFingerprint, err := readConfigFingerprint(bizPath)
	if err != nil {
		lg.Errorw("init business config fingerprint failed", "config", bizPath, "error", err)
		return 1
	}

	configChangeCh := make(chan struct{}, 1)
	watchDone := make(chan struct{})
	go watchConfigFile(bizPath, initialFingerprint, cfg.Control.ConfigWatchInterval, configChangeCh, watchDone)
	defer close(watchDone)

	logStartupInfo(lg, version, sysPath, bizPath, cfg)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, supportedSignals()...)
	defer signal.Stop(sigCh)

	for {
		select {
		case s := <-sigCh:
			if isReloadSignal(s) {
				lg.Infow("received reload signal", "signal", s.String())
				if next, ok := reloadAndApplyBusinessConfig(context.Background(), rt, sysPath, bizPath, "signal", s.String()); ok {
					lg.Infow("reload business config success", "signal", s.String(), "version", next.Version)
				}
				continue
			}

			if isStopSignal(s) {
				_ = rt.Stop(context.Background())
				shutdownPprofServer(pprofSrv, lg)
				lg.Info("forward-stub stopped.")
				return 0
			}

			lg.Infow("received unsupported signal", "signal", s.String())
		case <-configChangeCh:
			lg.Infow("detected business config file change", "config", bizPath)
			next, ok := reloadAndApplyBusinessConfig(context.Background(), rt, sysPath, bizPath, "source", "file-watch")
			if !ok {
				continue
			}
			lg.Infow("reload business config success", "source", "file-watch", "version", next.Version)
		}
	}
}

func logStartupInfo(lg interface {
	Infow(string, ...interface{})
	Info(...interface{})
}, version, systemPath, businessPath string, cfg config.Config) {
	lg.Infow("forward-stub startup",
		"version", version,
		"go_version", runtime.Version(),
		"gomaxprocs", runtime.GOMAXPROCS(0),
		"host_cpu_cores", runtime.NumCPU(),
		"pid", os.Getpid(),
		"system_config", systemPath,
		"business_config", businessPath,
		"config_version", cfg.Version,
	)
	lg.Info("forward-stub started. Press Ctrl+C to stop.")
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

func shutdownPprofServer(srv *http.Server, lg interface{ Warnw(string, ...interface{}) }) {
	if srv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		lg.Warnw("shutdown pprof http server failed", "addr", srv.Addr, "error", err)
	}
}

func watchConfigFile(path, initialFingerprint, watchInterval string, notifyCh chan<- struct{}, done <-chan struct{}) {
	lg := logx.L()
	currentFingerprint := initialFingerprint
	interval, err := time.ParseDuration(watchInterval)
	if err != nil || interval <= 0 {
		lg.Warnw("invalid config_watch_interval, fallback to default", "value", watchInterval, "default", config.DefaultConfigWatchInterval, "error", err)
		interval, _ = time.ParseDuration(config.DefaultConfigWatchInterval)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			nextFingerprint, err := readConfigFingerprint(path)
			if err != nil {
				lg.Warnw("watch config file failed", "config", path, "error", err)
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

func reloadAndApplyBusinessConfig(ctx context.Context, rt *app.Runtime, systemPath, businessPath, sourceKey, sourceValue string) (config.Config, bool) {
	lg := logx.L()
	systemCfg, _, next, err := loadConfigPair(ctx, systemPath, businessPath)
	if err != nil {
		lg.Errorw("reload config failed", sourceKey, sourceValue, "error", err)
		return config.Config{}, false
	}
	if err := rt.CheckSystemConfigStable(systemCfg); err != nil {
		lg.Errorw("reject business reload due to system config change", sourceKey, sourceValue, "error", err)
		return config.Config{}, false
	}
	if err := rt.UpdateCache(ctx, next); err != nil {
		lg.Errorw("apply reloaded config failed", sourceKey, sourceValue, "error", err)
		return config.Config{}, false
	}
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
