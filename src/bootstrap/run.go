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
//
// 整体职责：
// 1. 解析 system/business 配置并完成默认值与校验；
// 2. 初始化 logger、聚合统计参数、GC 日志、pprof；
// 3. 构建 runtime，并启动 receiver 进入稳定接流态；
// 4. 处理文件监听重载、信号重载与优雅停机。
func Run(args []string) int {
	bootLog := newStageLogger()
	bootLog.Info("process_start", "进程启动",
		"进程ID", os.Getpid(),
		"Go版本", goruntime.Version(),
		"GOMAXPROCS", goruntime.GOMAXPROCS(0),
		"主机CPU核心数", goruntime.NumCPU(),
	)

	fs := flag.NewFlagSet("forward-stub", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	systemPath := fs.String("system-config", "", "system config json path")
	businessPath := fs.String("business-config", "", "business config json path")
	if err := fs.Parse(args); err != nil {
		bootLog.Error("args_parse", "参数解析失败", err)
		return 2
	}
	bootLog.Info("args_parse", "参数解析完成", "参数数量", len(args))

	if *systemPath == "" || *businessPath == "" {
		err := fmt.Errorf("必须同时提供 -system-config 和 -business-config")
		bootLog.Error("config_path_resolve", "配置文件路径解析失败", err)
		return 1
	}
	sysPath, bizPath := *systemPath, *businessPath
	bootLog.Info("config_path_resolve", "配置文件路径解析完成", "系统配置文件", sysPath, "业务配置文件", bizPath)

	bootLog.Info("config_load", "开始加载配置文件")
	sysCfg, _, cfg, err := loadConfigPair(context.Background(), sysPath, bizPath)
	if err != nil {
		bootLog.Error("config_load", "配置文件加载失败", err, "系统配置文件", sysPath, "业务配置文件", bizPath)
		return 1
	}
	bootLog.Info("config_load", "配置文件加载完成")
	bootLog.Info("config_validate", "配置校验完成", "配置版本", cfg.Version)

	trafficStatsInterval, err := time.ParseDuration(cfg.Logging.TrafficStatsInterval)
	if err != nil {
		bootLog.Error("logger_init", "日志器初始化失败：流量统计间隔解析失败", err, "配置值", cfg.Logging.TrafficStatsInterval)
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
		bootLog.Error("logger_init", "日志器初始化失败", err)
		return 1
	}
	defer func() { _ = logx.Sync() }()
	bootLog.Attach(logx.L())
	bootLog.Info("logger_init", "日志器初始化完成", "日志级别", cfg.Logging.Level, "日志文件", cfg.Logging.File)
	logConfigSummary(bootLog, sysPath, bizPath, cfg)

	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()

	gcLogInterval, _ := time.ParseDuration(cfg.Logging.GCStatsLogInterval)
	stopGCStats := startGCStatsLogger(runCtx, bootLog, config.BoolValue(cfg.Logging.GCStatsLogEnabled), gcLogInterval)
	var stopGCOnce sync.Once
	stopGC := func() { stopGCOnce.Do(stopGCStats) }
	defer stopGC()

	bootLog.Info("pprof_init", "开始初始化pprof服务", "监听端口", cfg.Control.PprofPort)
	pprofSrv := startPprofServer(logx.L(), cfg.Control.PprofPort)
	var stopPprofOnce sync.Once
	stopPprof := func() { stopPprofOnce.Do(func() { shutdownPprofServer(pprofSrv, logx.L()) }) }
	defer stopPprof()

	rt := app.NewRuntime()
	bootLog.Info("runtime_init", "开始初始化运行时系统配置基线")
	if err := rt.SeedSystemConfig(sysCfg); err != nil {
		bootLog.Error("runtime_init", "运行时系统配置基线初始化失败", err)
		return 1
	}
	bootLog.Info("runtime_init", "运行时系统配置基线初始化完成")

	bootLog.Info("runtime_init", "开始初始化运行时组件", "配置版本", cfg.Version)
	if err := rt.UpdateCache(runCtx, cfg); err != nil {
		bootLog.Error("runtime_init", "运行时组件初始化失败", err)
		shutdownRuntime(rt, bootLog)
		return 1
	}
	bootLog.Info("runtime_init", "运行时组件初始化完成", "配置版本", cfg.Version)

	initialFingerprint, err := readConfigFingerprint(bizPath)
	if err != nil {
		bootLog.Error("config_watch", "业务配置指纹初始化失败", err, "业务配置文件", bizPath)
		shutdownRuntime(rt, bootLog)
		return 1
	}

	configChangeCh := make(chan struct{}, 1)
	watchCtx, cancelWatch := context.WithCancel(runCtx)
	defer cancelWatch()
	bootLog.Info("config_watch", "开始初始化业务配置监听任务", "业务配置文件", bizPath, "检查间隔", cfg.Control.ConfigWatchInterval)
	go watchConfigFile(watchCtx, bizPath, initialFingerprint, cfg.Control.ConfigWatchInterval, configChangeCh, bootLog)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, supportedSignals()...)
	defer signal.Stop(sigCh)
	bootLog.Info("signal_init", "信号处理器注册完成")
	bootLog.Info("service_start", "服务启动成功，开始接收流量", "系统配置文件", sysPath, "业务配置文件", bizPath, "配置版本", cfg.Version)

	for {
		select {
		case s := <-sigCh:
			if isReloadSignal(s) {
				bootLog.Info("reload_begin", "收到配置重载信号", "信号", s.String())
				if next, ok := reloadAndApplyBusinessConfig(runCtx, rt, sysPath, bizPath, bootLog, "signal", s.String()); ok {
					bootLog.Info("reload_done", "业务配置重载完成", "信号", s.String(), "配置版本", next.Version)
				}
				continue
			}

			if isStopSignal(s) {
				bootLog.Info("shutdown_begin", "开始优雅停机", "信号", s.String())
				cancelWatch()
				cancelRun()
				stopGC()
				shutdownRuntime(rt, bootLog)
				stopPprof()
				bootLog.Info("shutdown_done", "优雅停机完成", "信号", s.String())
				return 0
			}

			bootLog.Warn("signal_unhandled", "收到未处理信号", "信号", s.String())
		case <-configChangeCh:
			bootLog.Info("reload_begin", "检测到业务配置文件变更", "业务配置文件", bizPath)
			next, ok := reloadAndApplyBusinessConfig(runCtx, rt, sysPath, bizPath, bootLog, "source", "file-watch")
			if !ok {
				continue
			}
			bootLog.Info("reload_done", "业务配置重载完成", "来源", "file-watch", "配置版本", next.Version)
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
		lg.Infow("pprof请求处理完成",
			"方法", r.Method,
			"路径", r.URL.Path,
			"查询串", r.URL.RawQuery,
			"远端地址", r.RemoteAddr,
			"客户端", r.UserAgent(),
			"状态码", status,
			"响应字节数", rw.size,
			"耗时", time.Since(start).String(),
		)
	})
}

func startPprofServer(lg interface {
	Infow(string, ...interface{})
	Warnw(string, ...interface{})
}, port int) *http.Server {
	if port <= 0 {
		lg.Infow("pprof服务未启用", "监听端口", port)
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
			lg.Warnw("pprof服务异常退出", "监听地址", addr, "错误", err)
		}
	}()
	lg.Infow("pprof服务初始化完成", "监听地址", addr)
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
		lg.Warnw("pprof服务停止失败", "监听地址", srv.Addr, "错误", err)
		return
	}
	lg.Infow("pprof服务已停止", "监听地址", srv.Addr)
}

func watchConfigFile(ctx context.Context, path, initialFingerprint, watchInterval string, notifyCh chan<- struct{}, bootLog *stageLogger) {
	currentFingerprint := initialFingerprint
	interval, err := time.ParseDuration(watchInterval)
	if err != nil || interval <= 0 {
		bootLog.Warn("config_watch", "业务配置监听间隔无效，已回退到默认值", "配置值", watchInterval, "默认值", config.DefaultConfigWatchInterval, "错误", err)
		interval, _ = time.ParseDuration(config.DefaultConfigWatchInterval)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	bootLog.Info("config_watch", "业务配置监听任务已启动", "业务配置文件", path, "检查间隔", interval.String())
	defer bootLog.Info("config_watch", "业务配置监听任务已停止", "业务配置文件", path)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			nextFingerprint, err := readConfigFingerprint(path)
			if err != nil {
				bootLog.Warn("config_watch", "业务配置监听失败", "业务配置文件", path, "错误", err)
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

// reloadAndApplyBusinessConfig 执行一次业务配置热重载。
//
// 顺序：
// 1. 重新读取 system+business；
// 2. 拒绝 system 配置发生变化的场景；
// 3. 调用 runtime.UpdateCache 执行增量/全量切换；
// 4. 记录生效日志，供外部关联 reload_begin/reload_done。
func reloadAndApplyBusinessConfig(ctx context.Context, rt *app.Runtime, systemPath, businessPath string, bootLog *stageLogger, sourceKey, sourceValue string) (config.Config, bool) {
	systemCfg, _, next, err := loadConfigPair(ctx, systemPath, businessPath)
	if err != nil {
		bootLog.Error("reload_config", "业务配置重载失败：重新加载配置失败", err, sourceKey, sourceValue)
		return config.Config{}, false
	}
	if err := rt.CheckSystemConfigStable(systemCfg); err != nil {
		bootLog.Error("reload_config", "业务配置重载被拒绝：系统配置发生变化", err, sourceKey, sourceValue)
		return config.Config{}, false
	}
	if err := rt.UpdateCache(ctx, next); err != nil {
		bootLog.Error("reload_config", "业务配置重载失败：运行时应用新配置失败", err, sourceKey, sourceValue)
		return config.Config{}, false
	}
	bootLog.Info("reload_config", "业务配置重载已生效", sourceKey, sourceValue, "配置版本", next.Version)
	return next, true
}

// loadConfigPair 统一封装配置加载链路：
// 本地读取 -> 合并 -> ApplyDefaults -> 可选控制面拉取 -> 再次默认化 -> Validate。
//
// 这也是启动与热重载共享的配置入口，因此文档中的启动/重载时序都以此为准。
func loadConfigPair(ctx context.Context, systemPath, businessPath string) (config.SystemConfig, config.BusinessConfig, config.Config, error) {
	sys, biz, cfg, err := config.LoadLocalPair(systemPath, businessPath)
	if err != nil {
		return config.SystemConfig{}, config.BusinessConfig{}, config.Config{}, err
	}
	cfg.ApplyDefaults()
	if cfg.Control.API != "" {
		cli := control.NewConfigAPIClient(cfg.Control.API, cfg.Control.TimeoutSec)
		biz, err = cli.FetchBusinessConfig(ctx)
		if err != nil {
			return config.SystemConfig{}, config.BusinessConfig{}, config.Config{}, fmt.Errorf("通过配置中心拉取业务配置失败: %w", err)
		}
		cfg = sys.Merge(biz)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return config.SystemConfig{}, config.BusinessConfig{}, config.Config{}, fmt.Errorf("配置校验失败: %w", err)
	}
	return sys, biz, cfg, nil
}

// shutdownRuntime 在固定超时窗口内停止 runtime 全部组件。
// 停机顺序的细节由 runtime.Store.StopAll 决定，这里只负责提供统一超时与阶段日志。
func shutdownRuntime(rt *app.Runtime, bootLog *stageLogger) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := rt.Stop(ctx); err != nil {
		bootLog.Error("shutdown_runtime", "运行时停止失败", err, "超时时间", shutdownTimeout.String())
		return
	}
	bootLog.Info("shutdown_runtime", "运行时已停止", "超时时间", shutdownTimeout.String())
}
