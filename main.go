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
	localPath := flag.String("config", "", "local config json path (optional)")
	apiURL := flag.String("api", "", "java config service url (optional)")
	timeoutSec := flag.Int("timeout", 5, "api timeout seconds")
	logLevel := flag.String("log-level", "info", "log level: debug|info|warn|error")
	logFile := flag.String("log-file", "", "optional log file path (stdout when empty)")
	trafficStatsInterval := flag.Duration("traffic-stats-interval", time.Second, "aggregated traffic stats log interval (e.g. 5s, 10s)")
	trafficStatsSampleEvery := flag.Int("traffic-stats-sample-every", 1, "traffic stats sampling ratio N (record 1 every N packets)")
	trafficStatsEnableSender := flag.Bool("traffic-stats-enable-sender", true, "enable sender-dimension traffic stats")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		_, _ = os.Stdout.WriteString(version + "\n")
		return
	}

	if err := logx.Init(logx.Options{
		Level:                    *logLevel,
		File:                     *logFile,
		TrafficStatsInterval:     *trafficStatsInterval,
		TrafficStatsSampleEvery:  *trafficStatsSampleEvery,
		TrafficStatsEnableSender: *trafficStatsEnableSender,
	}); err != nil {
		_, _ = os.Stderr.WriteString("init logger error: " + err.Error() + "\n")
		os.Exit(1)
	}
	defer func() { _ = logx.Sync() }()
	lg := logx.L()

	var cfg config.Config
	var err error

	switch {
	case *localPath != "":
		cfg, err = config.LoadLocal(*localPath)
	case *apiURL != "":
		cli := control.NewConfigAPIClient(*apiURL, *timeoutSec)
		cfg, err = cli.FetchConfig(context.Background())
	default:
		lg.Error("must provide -config or -api")
		os.Exit(1)
	}

	if err != nil {
		lg.Errorf("load config error: %v", err)
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		lg.Errorf("config validate error: %v", err)
		os.Exit(1)
	}

	if cfg.Logging.Level == "" {
		cfg.Logging.Level = *logLevel
	}
	if cfg.Logging.File == "" {
		cfg.Logging.File = *logFile
	}
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
