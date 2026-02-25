// main.go 负责解析启动参数、加载配置来源并驱动运行时生命周期。
package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"forword-stub/src/app"
	"forword-stub/src/config"
	"forword-stub/src/control"
	"forword-stub/src/logx"
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

	defaultLogLevel := "info"
	if err := logx.Init(logx.Options{
		Level:                    defaultLogLevel,
		File:                     "",
		TrafficStatsInterval:     time.Second,
		TrafficStatsSampleEvery:  1,
		TrafficStatsEnableSender: true,
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
	if cfg.Control.API != "" {
		to := cfg.Control.TimeoutSec
		if to <= 0 {
			to = 5
		}
		cli := control.NewConfigAPIClient(cfg.Control.API, to)
		cfg, err = cli.FetchConfig(context.Background())
		if err != nil {
			lg.Errorf("fetch config from api error: %v", err)
			os.Exit(1)
		}
	}
	if err := cfg.Validate(); err != nil {
		lg.Errorf("config validate error: %v", err)
		os.Exit(1)
	}

	if cfg.Logging.Level == "" {
		cfg.Logging.Level = defaultLogLevel
	}
	if err := logx.Init(logx.Options{
		Level:                    cfg.Logging.Level,
		File:                     cfg.Logging.File,
		TrafficStatsInterval:     time.Second,
		TrafficStatsSampleEvery:  1,
		TrafficStatsEnableSender: true,
	}); err != nil {
		_, _ = os.Stderr.WriteString("re-init logger error: " + err.Error() + "\n")
		os.Exit(1)
	}
	lg = logx.L()
	if cfg.Logging.TrafficStatsInterval != "" {
		d, err := time.ParseDuration(cfg.Logging.TrafficStatsInterval)
		if err != nil {
			lg.Errorf("invalid traffic_stats_interval: %v", err)
			os.Exit(1)
		}
		logx.SetTrafficStatsInterval(d)
	}
	if cfg.Logging.TrafficStatsSampleEvery > 0 {
		logx.SetTrafficStatsSampleEvery(cfg.Logging.TrafficStatsSampleEvery)
	}
	if cfg.Logging.TrafficStatsEnableSender != nil {
		logx.SetTrafficStatsEnableSender(*cfg.Logging.TrafficStatsEnableSender)
	}

	rt := app.NewRuntime()
	if err := rt.UpdateCache(context.Background(), cfg); err != nil {
		lg.Errorf("UpdateCache error: %v", err)
		os.Exit(1)
	}

	lg.Info("forword-stub started. Press Ctrl+C to stop.")
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	_ = rt.Stop(context.Background())
	lg.Info("forword-stub stopped.")
}
