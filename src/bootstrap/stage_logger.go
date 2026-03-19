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
	fields := append([]interface{}{"阶段", stage}, kv...)
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
	fields := append([]interface{}{"阶段", stage}, kv...)
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
		kv = append(kv, "错误", err.Error())
	}
	fields := append([]interface{}{"阶段", stage}, kv...)
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
	l.Info("config_summary", "配置摘要已确认",
		"系统配置文件", systemPath,
		"业务配置文件", businessPath,
		"配置版本", cfg.Version,
		"接收端数量", len(cfg.Receivers),
		"选择器数量", len(cfg.Selectors),
		"任务集数量", len(cfg.TaskSets),
		"任务数量", len(cfg.Tasks),
		"发送端数量", len(cfg.Senders),
		"流水线数量", len(cfg.Pipelines),
		"GC周期日志启用", config.BoolValue(cfg.Logging.GCStatsLogEnabled),
		"GC周期日志间隔", cfg.Logging.GCStatsLogInterval,
	)

	receiverNames := sortedReceiverNames(cfg.Receivers)
	for _, name := range receiverNames {
		rc := cfg.Receivers[name]
		l.Info("receiver_config", "接收端配置已确认",
			"组件名称", name,
			"协议类型", rc.Type,
			"监听地址", rc.Listen,
			"选择器", rc.Selector,
			"分帧模式", rc.Frame,
			"event_loop数量", rc.NumEventLoop,
			"读缓冲上限", rc.ReadBufferCap,
			"socket接收缓冲", rc.SocketRecvBuffer,
			"Topic", rc.Topic,
			"消费组", rc.GroupID,
			"远端目录", rc.RemoteDir,
			"分块大小", rc.ChunkSize,
			"轮询间隔秒", rc.PollIntervalSec,
			"TLS启用", rc.TLS,
			"鉴权已配置", rc.Username != "" || rc.Password != "",
			"主机指纹已配置", rc.HostKeyFingerprint != "",
			"接收Payload日志", rc.LogPayloadRecv,
		)
	}

	senderNames := sortedSenderNames(cfg.Senders)
	for _, name := range senderNames {
		sc := cfg.Senders[name]
		l.Info("sender_config", "发送端配置已确认",
			"组件名称", name,
			"协议类型", sc.Type,
			"目标地址", sc.Remote,
			"分帧模式", sc.Frame,
			"并发度", sc.Concurrency,
			"socket发送缓冲", sc.SocketSendBuffer,
			"Topic", sc.Topic,
			"远端目录", sc.RemoteDir,
			"本地IP", sc.LocalIP,
			"本地端口", sc.LocalPort,
			"网卡", sc.Iface,
			"TLS启用", sc.TLS,
			"鉴权已配置", sc.Username != "" || sc.Password != "",
			"主机指纹已配置", sc.HostKeyFingerprint != "",
		)
	}

	selectorNames := sortedSelectorNames(cfg.Selectors)
	for _, name := range selectorNames {
		sc := cfg.Selectors[name]
		l.Info("selector_config", "选择器配置已确认",
			"组件名称", name,
			"匹配规则数量", len(sc.Matches),
			"默认任务集", sc.DefaultTaskSet,
		)
	}

	taskSetNames := sortedTaskSetNames(cfg.TaskSets)
	for _, name := range taskSetNames {
		l.Info("task_set_config", "任务集配置已确认",
			"组件名称", name,
			"任务列表", strings.Join(cfg.TaskSets[name], ","),
		)
	}

	taskNames := sortedTaskNames(cfg.Tasks)
	for _, name := range taskNames {
		tc := cfg.Tasks[name]
		l.Info("task_config", "任务配置已确认",
			"组件名称", name,
			"执行模型", tc.ExecutionModel,
			"协程池大小", tc.PoolSize,
			"排队长度", tc.QueueSize,
			"channel队列长度", tc.ChannelQueueSize,
			"流水线列表", strings.Join(tc.Pipelines, ","),
			"发送端列表", strings.Join(tc.Senders, ","),
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
