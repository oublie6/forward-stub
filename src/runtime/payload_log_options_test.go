// Package runtime 负责维护转发运行时对象及其测试辅助逻辑。
package runtime

import (
	"testing"

	"forward-stub/src/config"
)

// TestBuildTaskPayloadLogOptions 验证任务级 payload 日志开关与截断上限的优先级规则。
func TestBuildTaskPayloadLogOptions(t *testing.T) {
	lc := config.LoggingConfig{
		PayloadLogMaxBytes: 128,
	}
	tc := config.TaskConfig{LogPayloadSend: true}
	opt := buildTaskPayloadLogOptions(tc, lc)
	if !opt.send || opt.max != 128 {
		t.Fatalf("unexpected enabled options: %+v", opt)
	}

	opt = buildTaskPayloadLogOptions(config.TaskConfig{LogPayloadSend: false}, lc)
	if opt.send {
		t.Fatalf("send logging should follow task switch: %+v", opt)
	}

	opt = buildTaskPayloadLogOptions(config.TaskConfig{LogPayloadSend: true, PayloadLogMaxBytes: 32}, lc)
	if !opt.send || opt.max != 32 {
		t.Fatalf("task max bytes should override default: %+v", opt)
	}
}
