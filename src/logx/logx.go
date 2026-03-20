// logx.go 初始化全局日志器并提供等级判断辅助函数。
package logx

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/panjf2000/gnet/v2/pkg/logging"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Options 描述日志系统初始化参数。
//
// 其中 TrafficStatsInterval / TrafficStatsSampleEvery 会直接影响聚合统计日志：
// 前者决定后台 flush 周期，后者决定热路径计数采样倍率。
type Options struct {
	// Level 是主日志级别，同时会影响是否创建流量统计 counter（仅 info 及以上启用）。
	Level string
	// File 是日志输出文件；为空时输出到 stdout。
	File string
	// MaxSizeMB / MaxBackups / MaxAgeDays / Compress 控制滚动日志策略。
	MaxSizeMB  int
	MaxBackups int
	MaxAgeDays int
	Compress   bool
	// TrafficStatsInterval 是聚合统计日志周期。
	TrafficStatsInterval time.Duration
	// TrafficStatsSampleEvery 是流量统计热路径采样倍率，1 表示不采样。
	TrafficStatsSampleEvery int
}

var (
	mu          sync.RWMutex
	logger      = zap.NewNop().Sugar()
	atomicLevel = zap.NewAtomicLevelAt(zapcore.InfoLevel)
)

// Init 初始化全局 logger，并同步刷新聚合统计日志配置。
//
// 依赖的稳定第三方库：
//  1. zap：高性能结构化日志；
//  2. lumberjack：日志切割与压缩，避免单文件无限增长。
//
// 生命周期：通常在 bootstrap 冷启动阶段调用一次。
// 并发语义：该函数通过写锁替换全局 logger；替换后新的聚合计数器会读取最新配置。
func Init(opts Options) error {
	lvl, err := parseLevel(opts.Level)
	if err != nil {
		return err
	}
	if opts.MaxSizeMB <= 0 {
		opts.MaxSizeMB = 100
	}
	if opts.MaxBackups <= 0 {
		opts.MaxBackups = 5
	}
	if opts.MaxAgeDays <= 0 {
		opts.MaxAgeDays = 30
	}

	cfg := zap.NewProductionEncoderConfig()
	cfg.EncodeTime = zapcore.ISO8601TimeEncoder
	cfg.EncodeLevel = zapcore.CapitalLevelEncoder
	encoder := zapcore.NewConsoleEncoder(cfg)

	ws := zapcore.AddSync(os.Stdout)
	if opts.File != "" {
		ws = zapcore.AddSync(&lumberjack.Logger{
			Filename:   opts.File,
			MaxSize:    opts.MaxSizeMB,
			MaxBackups: opts.MaxBackups,
			MaxAge:     opts.MaxAgeDays,
			Compress:   opts.Compress,
		})
	}

	atomicLevel.SetLevel(lvl)
	SetTrafficStatsInterval(opts.TrafficStatsInterval)
	SetTrafficStatsSampleEvery(opts.TrafficStatsSampleEvery)
	core := zapcore.NewCore(encoder, ws, atomicLevel)
	z := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
	next := z.Sugar()

	mu.Lock()
	prev := logger
	logger = next
	mu.Unlock()

	// 将标准库 log 重定向到 zap，统一日志出口。
	log.SetOutput(zap.NewStdLog(next.Desugar()).Writer())
	_ = prev.Sync()
	return nil
}

// L 返回当前全局 sugar logger。
func L() *zap.SugaredLogger {
	mu.RLock()
	defer mu.RUnlock()
	return logger
}

// Sync 刷盘（进程退出前建议调用）。
func Sync() error {
	mu.RLock()
	defer mu.RUnlock()
	return logger.Sync()
}

// Enabled 判断某个日志级别当前是否会输出。
// receiver/task 会用它决定是否创建 TrafficCounter 或打印 payload 摘要，避免在关闭 info 时污染热路径。
func Enabled(level zapcore.Level) bool {
	return atomicLevel.Enabled(level)
}

// parseLevel 将配置字符串解析为 zap 日志级别，并对未知值返回显式错误。
func parseLevel(level string) (zapcore.Level, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "info":
		return zapcore.InfoLevel, nil
	case "debug":
		return zapcore.DebugLevel, nil
	case "warn", "warning":
		return zapcore.WarnLevel, nil
	case "error":
		return zapcore.ErrorLevel, nil
	default:
		return 0, fmt.Errorf("不支持的日志级别: %s", level)
	}
}

// ParseGnetLogLevel 将项目日志级别映射到 gnet 日志级别。
func ParseGnetLogLevel(level string) logging.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return logging.DebugLevel
	case "warn", "warning":
		return logging.WarnLevel
	case "error":
		return logging.ErrorLevel
	default:
		return logging.InfoLevel
	}
}
