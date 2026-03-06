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

	lastFingerprint, err := readConfigFingerprint(*localPath)
	if err != nil {
		lg.Errorw("init config fingerprint failed", "config", *localPath, "error", err)
		os.Exit(1)
	}

	configChangeCh := make(chan struct{}, 1)
	watchDone := make(chan struct{})
	go watchConfigFile(*localPath, lastFingerprint, configChangeCh, watchDone)
	defer close(watchDone)

	lg.Info("forward-stub started. Press Ctrl+C to stop.")
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, supportedSignals()...)
	defer signal.Stop(sigCh)

	for {
		select {
		case s := <-sigCh:
			if isReloadSignal(s) {
				lg.Infow("received reload signal", "signal", s.String())
				if next, ok := reloadAndApplyConfig(context.Background(), rt, *localPath, "signal", s.String()); ok {
					lastFingerprint, _ = readConfigFingerprint(*localPath)
					lg.Infow("reload config success", "signal", s.String(), "version", next.Version)
				}
				continue
			}

			if isStopSignal(s) {
				_ = rt.Stop(context.Background())
				lg.Info("forward-stub stopped.")
				return
			}

			lg.Infow("received unsupported signal", "signal", s.String())
		case <-configChangeCh:
			lg.Infow("detected config file change", "config", *localPath)
			next, ok := reloadAndApplyConfig(context.Background(), rt, *localPath, "source", "file-watch")
			if !ok {
				continue
			}
			fp, err := readConfigFingerprint(*localPath)
			if err != nil {
				lg.Errorw("refresh config fingerprint failed", "config", *localPath, "error", err)
				continue
			}
			lastFingerprint = fp
			lg.Infow("reload config success", "source", "file-watch", "version", next.Version)
		}
	}
}

func watchConfigFile(path, initialFingerprint string, notifyCh chan<- struct{}, done <-chan struct{}) {
	lg := logx.L()
	currentFingerprint := initialFingerprint
	ticker := time.NewTicker(configWatchInterval)
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

func reloadAndApplyConfig(ctx context.Context, rt *app.Runtime, localPath, sourceKey, sourceValue string) (config.Config, bool) {
	lg := logx.L()
	next, err := loadRuntimeConfig(localPath)
	if err != nil {
		lg.Errorw("reload config failed", sourceKey, sourceValue, "error", err)
		return config.Config{}, false
	}
	if err := rt.UpdateCache(ctx, next); err != nil {
		lg.Errorw("apply reloaded config failed", sourceKey, sourceValue, "error", err)
		return config.Config{}, false
	}
	return next, true
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
