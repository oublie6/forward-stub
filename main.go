// main.go 负责解析启动参数、加载配置来源并驱动运行时生命周期。
package main

import (
	"context"
	"flag"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"forward-stub/src/app"
	"forward-stub/src/config"
	"forward-stub/src/control"
	"forward-stub/src/logx"
)

var version = "dev"

// main 负责该函数对应的核心逻辑，详见实现细节。
func main() {
	localPath := flag.String("config", "", "local config json path")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		_, _ = os.Stdout.WriteString(version + "\n")
		return
	}

	if err := logx.Init(logx.Options{
		Level:                    config.DefaultLogLevel,
		File:                     "",
		MaxSizeMB:                config.DefaultLogRotateMaxSizeMB,
		MaxBackups:               config.DefaultLogRotateMaxBackups,
		MaxAgeDays:               config.DefaultLogRotateMaxAgeDays,
		Compress:                 config.DefaultLogRotateCompress,
		TrafficStatsInterval:     time.Second,
		TrafficStatsSampleEvery:  config.DefaultTrafficStatsSampleEvery,
		TrafficStatsEnableSender: config.DefaultTrafficStatsEnableSender,
	}); err != nil {
		_, _ = os.Stderr.WriteString("init logger error: " + err.Error() + "\n")
		os.Exit(1)
	}
	defer func() { _ = logx.Sync() }()
	lg := logx.L()

	var cfg config.Config
	var err error

	if *localPath == "" {
		lg.Error("must provide -config")
		os.Exit(1)
	}
	cfg, err = config.LoadLocal(*localPath)

	if err != nil {
		lg.Errorf("load config error: %v", err)
		os.Exit(1)
	}
	cfg.ApplyDefaults()
	if cfg.Control.API != "" {
		cli := control.NewConfigAPIClient(cfg.Control.API, cfg.Control.TimeoutSec)
		cfg, err = cli.FetchConfig(context.Background())
		if err != nil {
			lg.Errorf("fetch config from api error: %v", err)
			os.Exit(1)
		}
		cfg.ApplyDefaults()
	}
	if err := cfg.Validate(); err != nil {
		lg.Errorf("config validate error: %v", err)
		os.Exit(1)
	}

	trafficStatsInterval, err := time.ParseDuration(cfg.Logging.TrafficStatsInterval)
	if err != nil {
		lg.Errorf("invalid traffic_stats_interval: %v", err)
		os.Exit(1)
	}
	if err := logx.Init(logx.Options{
		Level:                    cfg.Logging.Level,
		File:                     cfg.Logging.File,
		MaxSizeMB:                cfg.Logging.MaxSizeMB,
		MaxBackups:               cfg.Logging.MaxBackups,
		MaxAgeDays:               cfg.Logging.MaxAgeDays,
		Compress:                 *cfg.Logging.Compress,
		TrafficStatsInterval:     trafficStatsInterval,
		TrafficStatsSampleEvery:  cfg.Logging.TrafficStatsSampleEvery,
		TrafficStatsEnableSender: *cfg.Logging.TrafficStatsEnableSender,
	}); err != nil {
		_, _ = os.Stderr.WriteString("re-init logger error: " + err.Error() + "\n")
		os.Exit(1)
	}
	lg = logx.L()

	var pprofSrv *http.Server
	if cfg.Pprof.Enabled {
		pprofSrv = &http.Server{Addr: cfg.Pprof.Listen}
		go func() {
			if err := pprofSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				lg.Warnw("pprof server stopped unexpectedly", "addr", cfg.Pprof.Listen, "error", err)
			}
		}()
		lg.Infow("pprof server enabled", "addr", cfg.Pprof.Listen)
	}

	rt := app.NewRuntime()
	if err := rt.UpdateCache(context.Background(), cfg); err != nil {
		lg.Errorf("UpdateCache error: %v", err)
		os.Exit(1)
	}

	lg.Info("forward-stub started. Press Ctrl+C to stop.")
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	if pprofSrv != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = pprofSrv.Shutdown(shutdownCtx)
		cancel()
	}
	_ = rt.Stop(context.Background())
	lg.Info("forward-stub stopped.")
}
