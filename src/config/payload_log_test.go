package config

import "testing"

func TestBuildTaskPayloadLogOptions(t *testing.T) {
	lc := LoggingConfig{PayloadLogMaxBytes: 128}
	opt := BuildTaskPayloadLogOptions(TaskConfig{LogPayloadSend: true}, lc)
	if !opt.Send || opt.Max != 128 {
		t.Fatalf("unexpected enabled options: %+v", opt)
	}

	opt = BuildTaskPayloadLogOptions(TaskConfig{LogPayloadSend: false}, lc)
	if opt.Send {
		t.Fatalf("send logging should follow task switch: %+v", opt)
	}

	opt = BuildTaskPayloadLogOptions(TaskConfig{LogPayloadSend: true, PayloadLogMaxBytes: 32}, lc)
	if !opt.Send || opt.Max != 32 {
		t.Fatalf("task max bytes should override default: %+v", opt)
	}
}
