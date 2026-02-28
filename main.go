// main.go 负责解析启动参数、加载配置来源并驱动运行时生命周期。
package main

import (
	"context"
	"flag"
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

	var cfg config.Config
	var err error

	if *localPath == "" {
		_, _ = os.Stderr.WriteString("must provide -config\n")
		os.Exit(1)
	}
	cfg, err = config.LoadLocal(*localPath)

	if err != nil {
		_, _ = os.Stderr.WriteString("load config error: " + err.Error() + "\n")
		os.Exit(1)
	}
	cfg.ApplyDefaults()
	if cfg.Control.API != "" {
		cli := control.NewConfigAPIClient(cfg.Control.API, cfg.Control.TimeoutSec)
		cfg, err = cli.FetchConfig(context.Background())
		if err != nil {
			_, _ = os.Stderr.WriteString("fetch config from api error: " + err.Error() + "\n")
			os.Exit(1)
		}
		cfg.ApplyDefaults()
	}
	if err := cfg.Validate(); err != nil {
		_, _ = os.Stderr.WriteString("config validate error: " + err.Error() + "\n")
		os.Exit(1)
	}

	trafficStatsInterval, err := time.ParseDuration(cfg.Logging.TrafficStatsInterval)
	if err != nil {
		_, _ = os.Stderr.WriteString("invalid traffic_stats_interval: " + err.Error() + "\n")
		os.Exit(1)
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
		os.Exit(1)
	}
	defer func() { _ = logx.Sync() }()
	lg := logx.L()

	rt := app.NewRuntime()
	if err := rt.UpdateCache(context.Background(), cfg); err != nil {
		lg.Errorf("UpdateCache error: %v", err)
		os.Exit(1)
	}

	lg.Info("forward-stub started. Press Ctrl+C to stop.")
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	_ = rt.Stop(context.Background())
	lg.Info("forward-stub stopped.")
}
