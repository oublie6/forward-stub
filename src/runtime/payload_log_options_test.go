package runtime

import (
	"testing"

	"forward-stub/src/config"
)

// TestBuildTaskPayloadLogOptions verifies the BuildTaskPayloadLogOptions behavior for the runtime package.
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
