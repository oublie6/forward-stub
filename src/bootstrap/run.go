package bootstrap

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"time"

	"forward-stub/src/app"
	"forward-stub/src/config"
	"forward-stub/src/control"
	"forward-stub/src/logx"
)

// Run 负责解析参数、加载配置并驱动运行时生命周期。
func Run(args []string) int {
	fs := flag.NewFlagSet("forward-stub", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	legacyPath := fs.String("config", "", "legacy config json path (same file for system and business)")
	systemPath := fs.String("system-config", "", "system config json path")
	businessPath := fs.String("business-config", "", "business config json path")
	prelog := stderrBootstrapLogger{out: os.Stderr}
	logBootstrapStepDone(prelog, "process_start", "pid", os.Getpid(), "args_count", len(args))
	if err := fs.Parse(args); err != nil {
		return 2
	}
	logBootstrapStepDone(prelog, "cli_parse", "legacy_config", emptyAsUnset(*legacyPath), "system_config", emptyAsUnset(*systemPath), "business_config", emptyAsUnset(*businessPath))

	logBootstrapStepStart(prelog, "resolve_config_paths")
	sysPath, bizPath, err := config.ResolveConfigPaths(*legacyPath, *systemPath, *businessPath)
	if err != nil {
		logBootstrapStepFailed(prelog, "resolve_config_paths", "error", err)
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		return 1
	}
	logBootstrapStepDone(prelog, "resolve_config_paths", "system_config", sysPath, "business_config", bizPath)

	logBootstrapStepStart(prelog, "load_config_pair", "system_config", sysPath, "business_config", bizPath)
	sysCfg, _, cfg, err := loadConfigPair(context.Background(), sysPath, bizPath, prelog)
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		return 1
	}
	logBootstrapStepDone(prelog, "load_config_pair", "config_version", cfg.Version)

	trafficStatsInterval, err := time.ParseDuration(cfg.Logging.TrafficStatsInterval)
	if err != nil {
		_, _ = os.Stderr.WriteString("invalid traffic_stats_interval: " + err.Error() + "\n")
		return 1
	}
	gcStatsInterval, err := time.ParseDuration(cfg.Logging.GCStatsInterval)
	if err != nil {
		_, _ = os.Stderr.WriteString("invalid gc_stats_interval: " + err.Error() + "\n")
		return 1
	}
	logBootstrapStepStart(prelog, "logger_init", "level", cfg.Logging.Level, "file", emptyAsUnset(cfg.Logging.File))
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
	logBootstrapStepDone(lg, "logger_init", "level", cfg.Logging.Level, "file", emptyAsUnset(cfg.Logging.File))

	stopGCStatsLogger := startGCStatsLogger(lg, cfg.Logging.GCStatsEnabled, gcStatsInterval)
	defer stopGCStatsLogger()

	logBootstrapStepStart(lg, "pprof_init", "port", cfg.Control.PprofPort)
	pprofSrv := startPprofServer(lg, cfg.Control.PprofPort)

	logBootstrapStepStart(lg, "runtime_create")
	rt := app.NewRuntime()
	logBootstrapStepDone(lg, "runtime_create")

	logBootstrapStepStart(lg, "runtime_update_cache", "config_version", cfg.Version)
	if err := rt.UpdateCache(context.Background(), cfg); err != nil {
		logBootstrapStepFailed(lg, "runtime_update_cache", "config_version", cfg.Version, "error", err)
		lg.Errorf("UpdateCache error: %v", err)
		return 1
	}
	logBootstrapStepDone(lg, "runtime_update_cache", "config_version", cfg.Version)

	logBootstrapStepStart(lg, "seed_system_config")
	if err := rt.SeedSystemConfig(sysCfg); err != nil {
		logBootstrapStepFailed(lg, "seed_system_config", "error", err)
		lg.Errorf("seed system config error: %v", err)
		return 1
	}
	logBootstrapStepDone(lg, "seed_system_config")

	initialFingerprint, err := readConfigFingerprint(bizPath)
	if err != nil {
		logBootstrapStepFailed(lg, "business_fingerprint_init", "config", bizPath, "error", err)
		lg.Errorw("init business config fingerprint failed", "config", bizPath, "error", err)
		return 1
	}
	logBootstrapStepDone(lg, "business_fingerprint_init", "config", bizPath, "fingerprint", initialFingerprint)

	configChangeCh := make(chan struct{}, 1)
	watchDone := make(chan struct{})
	logBootstrapStepStart(lg, "config_watcher_start", "config", bizPath, "interval", cfg.Control.ConfigWatchInterval)
	go watchConfigFile(bizPath, initialFingerprint, cfg.Control.ConfigWatchInterval, configChangeCh, watchDone)
	defer close(watchDone)
	logBootstrapStepDone(lg, "config_watcher_start", "config", bizPath, "interval", cfg.Control.ConfigWatchInterval)

	logStartupInfo(lg, sysPath, bizPath, cfg)
	sigCh := make(chan os.Signal, 1)
	signals := supportedSignals()
	signal.Notify(sigCh, signals...)
	defer signal.Stop(sigCh)
	logBootstrapStepDone(lg, "signal_listener_register", "signals", signalNames(signals))
	logBootstrapStepDone(lg, "startup_ready", "config_version", cfg.Version)

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

// logStartupInfo 是供 run.go 使用的包内辅助函数。
func logStartupInfo(lg interface {
	Infow(string, ...interface{})
	Info(...interface{})
}, systemPath, businessPath string, cfg config.Config) {
	lg.Infow("forward-stub startup",
		"go_version", runtime.Version(),
		"gomaxprocs", runtime.GOMAXPROCS(0),
		"host_cpu_cores", runtime.NumCPU(),
		"pid", os.Getpid(),
		"system_config", systemPath,
		"business_config", businessPath,
		"config_version", cfg.Version,
	)
	lg.Info("forward-stub startup completed, entering main loop. Press Ctrl+C to stop.")
}

// pprofResponseRecorder 是供 run.go 使用的包内辅助结构。
type pprofResponseRecorder struct {
	http.ResponseWriter
	status int
	size   int
}

// WriteHeader 提供运行时链路所需的 bootstrap 层行为。
func (r *pprofResponseRecorder) WriteHeader(statusCode int) {
	r.status = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

// Write 提供运行时链路所需的 bootstrap 层行为。
func (r *pprofResponseRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.size += n
	return n, err
}

// withPprofRequestLog 是供 run.go 使用的包内辅助函数。
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

// startPprofServer 是供 run.go 使用的包内辅助函数。
func startPprofServer(lg interface {
	Infow(string, ...interface{})
	Warnw(string, ...interface{})
}, port int) *http.Server {
	if port <= 0 {
		logBootstrapStepDone(lg, "pprof_init", "state", "disabled", "port", port)
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
	logBootstrapStepDone(lg, "pprof_init", "state", "enabled", "addr", addr)
	return srv
}

// shutdownPprofServer 是供 run.go 使用的包内辅助函数。
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

// watchConfigFile 是供 run.go 使用的包内辅助函数。
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

// reloadAndApplyBusinessConfig 是供 run.go 使用的包内辅助函数。
func reloadAndApplyBusinessConfig(ctx context.Context, rt *app.Runtime, systemPath, businessPath, sourceKey, sourceValue string) (config.Config, bool) {
	lg := logx.L()
	systemCfg, _, next, err := loadConfigPair(ctx, systemPath, businessPath, lg)
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

// loadConfigPair 是供 run.go 使用的包内辅助函数。
func loadConfigPair(ctx context.Context, systemPath, businessPath string, progress bootstrapStepLogger) (config.SystemConfig, config.BusinessConfig, config.Config, error) {
	logBootstrapStepStart(progress, "config_local_load", "system_config", systemPath, "business_config", businessPath)
	sys, biz, cfg, err := config.LoadLocalPair(systemPath, businessPath)
	if err != nil {
		logBootstrapStepFailed(progress, "config_local_load", "error", err)
		return config.SystemConfig{}, config.BusinessConfig{}, config.Config{}, err
	}
	logBootstrapStepDone(progress, "config_local_load", "system_config", systemPath, "business_config", businessPath, "config_version", cfg.Version)
	if cfg.Control.API != "" {
		logBootstrapStepStart(progress, "control_api_fetch", "api", cfg.Control.API, "timeout_sec", cfg.Control.TimeoutSec)
		cli := control.NewConfigAPIClient(cfg.Control.API, cfg.Control.TimeoutSec)
		biz, err = cli.FetchBusinessConfig(ctx)
		if err != nil {
			logBootstrapStepFailed(progress, "control_api_fetch", "api", cfg.Control.API, "error", err)
			return config.SystemConfig{}, config.BusinessConfig{}, config.Config{}, fmt.Errorf("fetch business config from api error: %w", err)
		}
		cfg = sys.Merge(biz)
		logBootstrapStepDone(progress, "control_api_fetch", "api", cfg.Control.API, "config_version", cfg.Version)
	} else {
		logBootstrapStepDone(progress, "control_api_fetch", "state", "disabled")
	}
	logBootstrapStepStart(progress, "config_apply_defaults")
	cfg.ApplyDefaults()
	logBootstrapStepDone(progress, "config_apply_defaults", "pprof_port", cfg.Control.PprofPort, "gc_stats_enabled", cfg.Logging.GCStatsEnabled, "gc_stats_interval", cfg.Logging.GCStatsInterval)
	logBootstrapStepStart(progress, "config_validate")
	if err := cfg.Validate(); err != nil {
		logBootstrapStepFailed(progress, "config_validate", "error", err)
		return config.SystemConfig{}, config.BusinessConfig{}, config.Config{}, fmt.Errorf("config validate error: %w", err)
	}
	logBootstrapStepDone(progress, "config_validate", "receivers", len(cfg.Receivers), "tasks", len(cfg.Tasks), "senders", len(cfg.Senders))
	return sys, biz, cfg, nil
}

type bootstrapStepLogger interface {
	Infow(string, ...interface{})
	Warnw(string, ...interface{})
}

type stderrBootstrapLogger struct {
	out io.Writer
}

func (l stderrBootstrapLogger) Infow(msg string, keysAndValues ...interface{}) {
	writeBootstrapLogLine(l.out, "INFO", msg, keysAndValues...)
}

func (l stderrBootstrapLogger) Warnw(msg string, keysAndValues ...interface{}) {
	writeBootstrapLogLine(l.out, "WARN", msg, keysAndValues...)
}

func writeBootstrapLogLine(out io.Writer, level, msg string, keysAndValues ...interface{}) {
	if out == nil {
		return
	}
	_, _ = fmt.Fprintln(out, formatBootstrapLogLine(level, msg, keysAndValues...))
}

func formatBootstrapLogLine(level, msg string, keysAndValues ...interface{}) string {
	var b strings.Builder
	b.WriteString(level)
	b.WriteString(" ")
	b.WriteString(msg)
	for i := 0; i+1 < len(keysAndValues); i += 2 {
		b.WriteByte(' ')
		b.WriteString(fmt.Sprint(keysAndValues[i]))
		b.WriteByte('=')
		b.WriteString(fmt.Sprint(keysAndValues[i+1]))
	}
	if len(keysAndValues)%2 == 1 {
		b.WriteString(" extra=")
		b.WriteString(fmt.Sprint(keysAndValues[len(keysAndValues)-1]))
	}
	return b.String()
}

func logBootstrapStepStart(l bootstrapStepLogger, step string, keysAndValues ...interface{}) {
	if l == nil {
		return
	}
	l.Infow("bootstrap step starting", append([]interface{}{"step", step}, keysAndValues...)...)
}

func logBootstrapStepDone(l bootstrapStepLogger, step string, keysAndValues ...interface{}) {
	if l == nil {
		return
	}
	l.Infow("bootstrap step completed", append([]interface{}{"step", step}, keysAndValues...)...)
}

func logBootstrapStepFailed(l bootstrapStepLogger, step string, keysAndValues ...interface{}) {
	if l == nil {
		return
	}
	l.Warnw("bootstrap step failed", append([]interface{}{"step", step}, keysAndValues...)...)
}

func emptyAsUnset(v string) string {
	if strings.TrimSpace(v) == "" {
		return "unset"
	}
	return v
}

func signalNames(signals []os.Signal) string {
	names := make([]string, 0, len(signals))
	for _, sig := range signals {
		names = append(names, sig.String())
	}
	return strings.Join(names, ",")
}

func startGCStatsLogger(lg bootstrapStepLogger, enabled bool, interval time.Duration) func() {
	if !enabled {
		logBootstrapStepDone(lg, "gc_stats_logger_start", "state", "disabled")
		return func() {}
	}
	logBootstrapStepStart(lg, "gc_stats_logger_start", "interval", interval.String())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		var prev runtime.MemStats
		runtime.ReadMemStats(&prev)
		logBootstrapStepDone(lg, "gc_stats_logger_start", "state", "enabled", "interval", interval.String())

		for {
			select {
			case <-ctx.Done():
				logBootstrapStepDone(lg, "gc_stats_logger_stop")
				return
			case <-ticker.C:
				var cur runtime.MemStats
				runtime.ReadMemStats(&cur)
				fields := []interface{}{
					"num_gc", cur.NumGC,
					"gc_count_delta", cur.NumGC - prev.NumGC,
					"pause_total_ns", cur.PauseTotalNs,
					"pause_total_delta_ns", cur.PauseTotalNs - prev.PauseTotalNs,
					"heap_alloc", cur.HeapAlloc,
					"heap_inuse", cur.HeapInuse,
					"heap_sys", cur.HeapSys,
					"heap_objects", cur.HeapObjects,
					"next_gc", cur.NextGC,
					"gc_cpu_fraction", cur.GCCPUFraction,
				}
				if cur.LastGC > 0 {
					fields = append(fields, "last_gc", time.Unix(0, int64(cur.LastGC)).UTC().Format(time.RFC3339Nano))
				}
				lg.Infow("gc stats", fields...)
				prev = cur
			}
		}
	}()

	return func() {
		cancel()
		<-done
	}
}
