package bootstrap

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"forward-stub/src/config"
)

type structuredLogger interface {
	Infow(string, ...interface{})
	Errorw(string, ...interface{})
	Warnw(string, ...interface{})
}

type stageLogger struct {
	mu sync.RWMutex
	lg structuredLogger
}

func newStageLogger() *stageLogger {
	return &stageLogger{}
}

func (l *stageLogger) Attach(lg structuredLogger) {
	l.mu.Lock()
	l.lg = lg
	l.mu.Unlock()
}

func (l *stageLogger) Info(stage, msg string, kv ...interface{}) {
	fields := append([]interface{}{"stage", stage}, kv...)
	l.mu.RLock()
	logger := l.lg
	l.mu.RUnlock()
	if logger != nil {
		logger.Infow(msg, fields...)
		return
	}
	writeBootstrapLine("INFO", stage, msg, kv...)
}

func (l *stageLogger) Warn(stage, msg string, kv ...interface{}) {
	fields := append([]interface{}{"stage", stage}, kv...)
	l.mu.RLock()
	logger := l.lg
	l.mu.RUnlock()
	if logger != nil {
		logger.Warnw(msg, fields...)
		return
	}
	writeBootstrapLine("WARN", stage, msg, kv...)
}

func (l *stageLogger) Error(stage, msg string, err error, kv ...interface{}) {
	if err != nil {
		kv = append(kv, "error", err.Error())
	}
	fields := append([]interface{}{"stage", stage}, kv...)
	l.mu.RLock()
	logger := l.lg
	l.mu.RUnlock()
	if logger != nil {
		logger.Errorw(msg, fields...)
		return
	}
	writeBootstrapLine("ERROR", stage, msg, kv...)
}

func writeBootstrapLine(level, stage, msg string, kv ...interface{}) {
	var b strings.Builder
	b.WriteString(time.Now().Format(time.RFC3339))
	b.WriteString(" level=")
	b.WriteString(level)
	b.WriteString(" stage=")
	b.WriteString(stage)
	b.WriteString(" msg=")
	b.WriteString(strconvQuote(msg))
	for i := 0; i+1 < len(kv); i += 2 {
		b.WriteByte(' ')
		b.WriteString(fmt.Sprint(kv[i]))
		b.WriteByte('=')
		b.WriteString(strconvQuote(fmt.Sprint(kv[i+1])))
	}
	b.WriteByte('\n')
	_, _ = os.Stderr.WriteString(b.String())
}

func strconvQuote(v string) string {
	return fmt.Sprintf("%q", v)
}

func logConfigSummary(l *stageLogger, systemPath, businessPath string, cfg config.Config) {
	l.Info("config_summary", "runtime config summary",
		"system_config", systemPath,
		"business_config", businessPath,
		"config_version", cfg.Version,
		"receivers", len(cfg.Receivers),
		"selectors", len(cfg.Selectors),
		"task_sets", len(cfg.TaskSets),
		"tasks", len(cfg.Tasks),
		"senders", len(cfg.Senders),
		"pipelines", len(cfg.Pipelines),
		"gc_stats_log_enabled", config.BoolValue(cfg.Logging.GCStatsLogEnabled),
		"gc_stats_log_interval", cfg.Logging.GCStatsLogInterval,
	)

	receiverNames := sortedReceiverNames(cfg.Receivers)
	for _, name := range receiverNames {
		rc := cfg.Receivers[name]
		l.Info("receiver_config", "receiver config ready",
			"name", name,
			"type", rc.Type,
			"listen", rc.Listen,
			"selector", rc.Selector,
			"frame", rc.Frame,
			"event_loops", rc.NumEventLoop,
			"read_buffer_cap", rc.ReadBufferCap,
			"socket_recv_buffer", rc.SocketRecvBuffer,
			"topic", rc.Topic,
			"group_id", rc.GroupID,
			"remote_dir", rc.RemoteDir,
			"chunk_size", rc.ChunkSize,
			"poll_interval_sec", rc.PollIntervalSec,
			"tls", rc.TLS,
			"auth_enabled", rc.Username != "" || rc.Password != "",
			"host_key_fingerprint_set", rc.HostKeyFingerprint != "",
			"log_payload_recv", rc.LogPayloadRecv,
		)
	}

	senderNames := sortedSenderNames(cfg.Senders)
	for _, name := range senderNames {
		sc := cfg.Senders[name]
		l.Info("sender_config", "sender config ready",
			"name", name,
			"type", sc.Type,
			"remote", sc.Remote,
			"frame", sc.Frame,
			"concurrency", sc.Concurrency,
			"socket_send_buffer", sc.SocketSendBuffer,
			"topic", sc.Topic,
			"remote_dir", sc.RemoteDir,
			"local_ip", sc.LocalIP,
			"local_port", sc.LocalPort,
			"iface", sc.Iface,
			"tls", sc.TLS,
			"auth_enabled", sc.Username != "" || sc.Password != "",
			"host_key_fingerprint_set", sc.HostKeyFingerprint != "",
		)
	}

	selectorNames := sortedSelectorNames(cfg.Selectors)
	for _, name := range selectorNames {
		sc := cfg.Selectors[name]
		l.Info("selector_config", "selector config ready",
			"name", name,
			"match_count", len(sc.Matches),
			"default_task_set", sc.DefaultTaskSet,
		)
	}

	taskSetNames := sortedTaskSetNames(cfg.TaskSets)
	for _, name := range taskSetNames {
		l.Info("task_set_config", "task set config ready",
			"name", name,
			"tasks", strings.Join(cfg.TaskSets[name], ","),
		)
	}

	taskNames := sortedTaskNames(cfg.Tasks)
	for _, name := range taskNames {
		tc := cfg.Tasks[name]
		l.Info("task_config", "task config ready",
			"name", name,
			"execution_model", tc.ExecutionModel,
			"pool_size", tc.PoolSize,
			"queue_size", tc.QueueSize,
			"channel_queue_size", tc.ChannelQueueSize,
			"pipelines", strings.Join(tc.Pipelines, ","),
			"senders", strings.Join(tc.Senders, ","),
		)
	}
}

func sortedReceiverNames(m map[string]config.ReceiverConfig) []string {
	out := make([]string, 0, len(m))
	for name := range m {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func sortedSenderNames(m map[string]config.SenderConfig) []string {
	out := make([]string, 0, len(m))
	for name := range m {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func sortedSelectorNames(m map[string]config.SelectorConfig) []string {
	out := make([]string, 0, len(m))
	for name := range m {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func sortedTaskSetNames(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for name := range m {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func sortedTaskNames(m map[string]config.TaskConfig) []string {
	out := make([]string, 0, len(m))
	for name := range m {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
