// main.go 负责解析启动参数、加载配置来源并驱动运行时生命周期。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
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

	if *localPath == "" {
		_, _ = os.Stderr.WriteString("must provide -config\n")
		os.Exit(1)
	}

	cfg, err := loadRuntimeConfig(*localPath)
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
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
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, supportedSignals()...)
	defer signal.Stop(sigCh)

	for {
		s := <-sigCh
		if isReloadSignal(s) {
			lg.Infow("received reload signal", "signal", s.String())
			next, err := loadRuntimeConfig(*localPath)
			if err != nil {
				lg.Errorw("reload config failed", "signal", s.String(), "error", err)
				continue
			}
			if err := rt.UpdateCache(context.Background(), next); err != nil {
				lg.Errorw("apply reloaded config failed", "signal", s.String(), "error", err)
				continue
			}
			lg.Infow("reload config success", "signal", s.String(), "version", next.Version)
			continue
		}

		if isStopSignal(s) {
			_ = rt.Stop(context.Background())
			lg.Info("forward-stub stopped.")
			return
		}

		lg.Infow("received unsupported signal", "signal", s.String())
	}
}

func loadRuntimeConfig(localPath string) (config.Config, error) {
	cfg, err := config.LoadLocal(localPath)
	if err != nil {
		return config.Config{}, fmt.Errorf("load config error: %w", err)
	}
	cfg.ApplyDefaults()
	if cfg.Control.API != "" {
		cli := control.NewConfigAPIClient(cfg.Control.API, cfg.Control.TimeoutSec)
		cfg, err = cli.FetchConfig(context.Background())
		if err != nil {
			return config.Config{}, fmt.Errorf("fetch config from api error: %w", err)
		}
		cfg.ApplyDefaults()
	}
	if err := cfg.Validate(); err != nil {
		return config.Config{}, fmt.Errorf("config validate error: %w", err)
	}
	return cfg, nil
}
