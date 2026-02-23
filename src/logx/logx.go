package logx

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/panjf2000/gnet/v2/pkg/logging"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Options 为日志初始化参数。
type Options struct {
	Level string
	File  string
}

var (
	mu          sync.RWMutex
	logger      = zap.NewNop().Sugar()
	atomicLevel = zap.NewAtomicLevelAt(zapcore.InfoLevel)
)

// Init 初始化全局 logger。
//
// 依赖的稳定第三方库：
//  1. zap：高性能结构化日志；
//  2. lumberjack：日志切割与压缩，避免单文件无限增长。
func Init(opts Options) error {
	lvl, err := parseLevel(opts.Level)
	if err != nil {
		return err
	}

	cfg := zap.NewProductionEncoderConfig()
	cfg.EncodeTime = zapcore.ISO8601TimeEncoder
	cfg.EncodeLevel = zapcore.CapitalLevelEncoder
	encoder := zapcore.NewConsoleEncoder(cfg)

	ws := zapcore.AddSync(os.Stdout)
	if opts.File != "" {
		ws = zapcore.AddSync(&lumberjack.Logger{
			Filename:   opts.File,
			MaxSize:    100,
			MaxBackups: 5,
			MaxAge:     30,
			Compress:   true,
		})
	}

	atomicLevel.SetLevel(lvl)
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

func Enabled(level zapcore.Level) bool {
	return atomicLevel.Enabled(level)
}

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
		return 0, fmt.Errorf("unsupported log level: %s", level)
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
