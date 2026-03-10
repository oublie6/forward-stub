package config

import "testing"

func TestSystemBusinessDefaultsApplyOnMerge(t *testing.T) {
	sys := SystemConfig{
		BusinessDefaults: BusinessDefaultsConfig{
			Receiver: ReceiverConfig{Frame: "u16be", NumEventLoop: 3, PayloadLogMaxBytes: 320},
			Sender:   SenderConfig{Concurrency: 5},
			Task:     TaskConfig{ExecutionModel: "channel", QueueSize: 2048, ChannelQueueSize: 1024, PayloadLogMaxBytes: 480},
		},
	}
	biz := BusinessConfig{
		Receivers: map[string]ReceiverConfig{"r": {Type: "tcp_gnet", Listen: "127.0.0.1:9000"}},
		Senders:   map[string]SenderConfig{"s": {Type: "tcp_gnet", Remote: "127.0.0.1:9100"}},
		Tasks:     map[string]TaskConfig{"t": {Receivers: []string{"r"}, Senders: []string{"s"}}},
	}
	cfg := sys.Merge(biz)
	if cfg.Receivers["r"].Frame != "u16be" || cfg.Receivers["r"].NumEventLoop != 3 || cfg.Receivers["r"].PayloadLogMaxBytes != 320 {
		t.Fatalf("receiver defaults not applied: %+v", cfg.Receivers["r"])
	}
	if cfg.Senders["s"].Concurrency != 5 {
		t.Fatalf("sender defaults not applied: %+v", cfg.Senders["s"])
	}
	if cfg.Tasks["t"].ExecutionModel != "channel" || cfg.Tasks["t"].QueueSize != 2048 || cfg.Tasks["t"].ChannelQueueSize != 1024 || cfg.Tasks["t"].PayloadLogMaxBytes != 480 {
		t.Fatalf("task defaults not applied: %+v", cfg.Tasks["t"])
	}
}
