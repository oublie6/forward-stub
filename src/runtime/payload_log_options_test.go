package runtime

import (
	"testing"

	"forward-stub/src/config"
)

func TestBuildTaskPayloadLogOptions(t *testing.T) {
	lc := config.LoggingConfig{
		PayloadLogTasks:    []string{"t1"},
		PayloadLogRecv:     true,
		PayloadLogSend:     true,
		PayloadLogMaxBytes: 128,
	}
	tc := config.TaskConfig{LogPayloadRecv: true, LogPayloadSend: true}
	opt := buildTaskPayloadLogOptions("t1", tc, lc)
	if !opt.recv || !opt.send || opt.max != 128 {
		t.Fatalf("unexpected enabled options: %+v", opt)
	}

	opt = buildTaskPayloadLogOptions("t2", tc, lc)
	if opt.recv || opt.send {
		t.Fatalf("task not in whitelist should be disabled: %+v", opt)
	}

	lc.PayloadLogTasks = nil
	opt = buildTaskPayloadLogOptions("t2", tc, lc)
	if opt.recv || opt.send {
		t.Fatalf("empty whitelist should keep disabled by default: %+v", opt)
	}
}
