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

type Options struct {
	Level string
	File  string
}

var (
	mu          sync.RWMutex
	logger      = zap.NewNop().Sugar()
	atomicLevel = zap.NewAtomicLevelAt(zapcore.InfoLevel)
)

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

	log.SetOutput(zap.NewStdLog(next.Desugar()).Writer())
	_ = prev.Sync()
	return nil
}

func L() *zap.SugaredLogger {
	mu.RLock()
	defer mu.RUnlock()
	return logger
}

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
