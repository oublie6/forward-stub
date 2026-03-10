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
	legacyPath := flag.String("config", "", "legacy config json path (same file for system and business)")
	systemPath := flag.String("system-config", "", "system config json path")
	businessPath := flag.String("business-config", "", "business config json path")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		_, _ = os.Stdout.WriteString(version + "\n")
		return
	}

	sysPath, bizPath, err := config.ResolveConfigPaths(*legacyPath, *systemPath, *businessPath)
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}

	sysCfg, bizCfg, cfg, err := loadConfigPair(context.Background(), sysPath, bizPath)
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
	if err := rt.SeedSystemConfig(sysCfg); err != nil {
		lg.Errorf("seed system config error: %v", err)
		os.Exit(1)
	}

	initialFingerprint, err := readConfigFingerprint(bizPath)
	if err != nil {
		lg.Errorw("init business config fingerprint failed", "config", bizPath, "error", err)
		os.Exit(1)
	}

	configChangeCh := make(chan struct{}, 1)
	watchDone := make(chan struct{})
	go watchConfigFile(bizPath, initialFingerprint, cfg.Control.ConfigWatchInterval, configChangeCh, watchDone)
	defer close(watchDone)

	_ = bizCfg
	lg.Info("forward-stub started. Press Ctrl+C to stop.")
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
				lg.Info("forward-stub stopped.")
				return
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
	systemCfg, businessCfg, next, err := loadConfigPair(ctx, systemPath, businessPath)
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
	_ = businessCfg
	return next, true
}

func loadConfigPair(ctx context.Context, systemPath, businessPath string) (config.SystemConfig, config.BusinessConfig, config.Config, error) {
	sys, biz, cfg, err := config.LoadLocalPair(systemPath, businessPath)
	if err != nil {
		return config.SystemConfig{}, config.BusinessConfig{}, config.Config{}, err
	}
	if cfg.Control.API != "" {
		cli := control.NewConfigAPIClient(cfg.Control.API, cfg.Control.TimeoutSec)
		cfg, err = cli.FetchConfig(ctx)
		if err != nil {
			return config.SystemConfig{}, config.BusinessConfig{}, config.Config{}, fmt.Errorf("fetch config from api error: %w", err)
		}
		cfg.ApplyDefaults()
	}
	if err := cfg.Validate(); err != nil {
		return config.SystemConfig{}, config.BusinessConfig{}, config.Config{}, fmt.Errorf("config validate error: %w", err)
	}
	return sys, biz, cfg, nil
}
