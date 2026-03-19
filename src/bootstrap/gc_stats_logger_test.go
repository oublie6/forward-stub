package bootstrap

import (
	"strings"
	"sync"
	"testing"
	"time"
)

type testStepLogger struct {
	mu      sync.Mutex
	entries []string
	ch      chan string
}

func newTestStepLogger() *testStepLogger {
	return &testStepLogger{ch: make(chan string, 32)}
}

func (l *testStepLogger) Infow(msg string, keysAndValues ...interface{}) {
	l.record(msg, keysAndValues...)
}

func (l *testStepLogger) Warnw(msg string, keysAndValues ...interface{}) {
	l.record(msg, keysAndValues...)
}

func (l *testStepLogger) Errorw(msg string, keysAndValues ...interface{}) {
	l.record(msg, keysAndValues...)
}

func (l *testStepLogger) record(msg string, keysAndValues ...interface{}) {
	line := formatBootstrapLogLine("TEST", msg, keysAndValues...)
	l.mu.Lock()
	l.entries = append(l.entries, line)
	l.mu.Unlock()
	select {
	case l.ch <- line:
	default:
	}
}

func (l *testStepLogger) contains(substr string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, entry := range l.entries {
		if strings.Contains(entry, substr) {
			return true
		}
	}
	return false
}

func waitForLogContains(t *testing.T, l *testStepLogger, substr string) {
	t.Helper()
	deadline := time.After(250 * time.Millisecond)
	for {
		if l.contains(substr) {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for log containing %q", substr)
		case <-l.ch:
		}
	}
}

// TestStartGCStatsLoggerDisabled 验证 bootstrap 包中 StartGCStatsLoggerDisabled 的行为。
func TestStartGCStatsLoggerDisabled(t *testing.T) {
	lg := newTestStepLogger()
	stop := startGCStatsLogger(lg, false, time.Minute)
	stop()

	if !lg.contains("GC 周期日志未启用") {
		t.Fatalf("expected disabled gc stats logger log, got %+v", lg.entries)
	}
	if lg.contains("GC 周期统计") {
		t.Fatalf("disabled gc stats logger should not emit stats logs")
	}
}

// TestStartGCStatsLoggerEnabledAndStops 验证 bootstrap 包中 StartGCStatsLoggerEnabledAndStops 的行为。
func TestStartGCStatsLoggerEnabledAndStops(t *testing.T) {
	lg := newTestStepLogger()
	stop := startGCStatsLogger(lg, true, 10*time.Millisecond)
	defer stop()

	waitForLogContains(t, lg, "GC 周期日志已启用")
	waitForLogContains(t, lg, "GC 周期统计")

	stop()
	waitForLogContains(t, lg, "GC 周期日志已停止")
}
