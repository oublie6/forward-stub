package config

// TaskPayloadLogOptions 描述 task 侧 payload 日志行为。
type TaskPayloadLogOptions struct {
	Send bool
	Max  int
}

// ResolvePayloadLogMax 返回局部配置与 logging 默认值合并后的 payload 摘要长度。
func ResolvePayloadLogMax(localMax int, loggingMax int) int {
	if localMax > 0 {
		return localMax
	}
	return loggingMax
}

// BuildTaskPayloadLogOptions 负责构建 task payload 日志选项。
func BuildTaskPayloadLogOptions(tc TaskConfig, lc LoggingConfig) TaskPayloadLogOptions {
	return TaskPayloadLogOptions{
		Send: tc.LogPayloadSend,
		Max:  ResolvePayloadLogMax(tc.PayloadLogMaxBytes, lc.PayloadLogMaxBytes),
	}
}
