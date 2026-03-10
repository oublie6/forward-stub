package config

import "testing"

func TestSystemBusinessDefaultsAppliedToBusinessConfig(t *testing.T) {
	m := true
	sys := SystemConfig{
		BusinessDefaults: BusinessDefaultsConfig{
			Task:     TaskDefaultConfig{PoolSize: 100, QueueSize: 200, ChannelQueueSize: 300, ExecutionModel: "channel", PayloadLogMaxBytes: 111},
			Receiver: ReceiverDefaultConfig{Multicore: &m, NumEventLoop: 12, PayloadLogMaxBytes: 222},
			Sender:   SenderDefaultConfig{Concurrency: 16},
		},
	}
	biz := BusinessConfig{
		Version:   1,
		Receivers: map[string]ReceiverConfig{"r1": {Type: "udp_gnet", Listen: ":9000"}},
		Senders:   map[string]SenderConfig{"s1": {Type: "tcp_gnet", Remote: "127.0.0.1:9100"}},
		Pipelines: map[string][]StageConfig{"p1": {}},
		Tasks:     map[string]TaskConfig{"t1": {Receivers: []string{"r1"}, Pipelines: []string{"p1"}, Senders: []string{"s1"}}},
	}

	cfg := sys.Merge(biz)
	cfg.ApplyDefaults()

	tc := cfg.Tasks["t1"]
	if tc.PoolSize != 100 || tc.QueueSize != 200 || tc.ChannelQueueSize != 300 || tc.ExecutionModel != "channel" || tc.PayloadLogMaxBytes != 111 {
		t.Fatalf("unexpected task defaults: %+v", tc)
	}
	rc := cfg.Receivers["r1"]
	if !rc.Multicore || rc.NumEventLoop != 12 || rc.PayloadLogMaxBytes != 222 {
		t.Fatalf("unexpected receiver defaults: %+v", rc)
	}
	sc := cfg.Senders["s1"]
	if sc.Concurrency != 16 {
		t.Fatalf("unexpected sender defaults: %+v", sc)
	}
}
