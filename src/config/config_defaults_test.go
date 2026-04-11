package config

import "testing"

func TestApplyDefaultsSetsControlDefaults(t *testing.T) {
	cfg := Config{}
	cfg.ApplyDefaults(BusinessDefaultsConfig{})
	if cfg.Control.ConfigWatchInterval != DefaultConfigWatchInterval {
		t.Fatalf("unexpected config_watch_interval default: %q", cfg.Control.ConfigWatchInterval)
	}
	if cfg.Control.PprofPort != DefaultPprofPort {
		t.Fatalf("unexpected pprof_port default: %d", cfg.Control.PprofPort)
	}
}

func TestApplyDefaultsKeepsDisabledPprofPort(t *testing.T) {
	cfg := Config{Control: ControlConfig{PprofPort: -1}}
	cfg.ApplyDefaults(BusinessDefaultsConfig{})
	if cfg.Control.PprofPort != -1 {
		t.Fatalf("unexpected pprof_port after defaults: %d", cfg.Control.PprofPort)
	}
}

func TestApplyDefaultsSetsReceiverMulticoreWhenUnset(t *testing.T) {
	cfg := Config{Receivers: map[string]ReceiverConfig{"r1": {Type: "udp_gnet", Listen: ":9000"}}}
	cfg.ApplyDefaults(BusinessDefaultsConfig{})
	rc := cfg.Receivers["r1"]
	if rc.Multicore == nil || !*rc.Multicore {
		t.Fatalf("unexpected receiver multicore default: %+v", rc.Multicore)
	}
}

func TestApplyDefaultsSetsTaskChannelQueueSizeIndependently(t *testing.T) {
	cfg := Config{
		Tasks: map[string]TaskConfig{
			"t1": {ExecutionModel: "channel"},
		},
	}
	cfg.ApplyDefaults(BusinessDefaultsConfig{})
	if got := cfg.Tasks["t1"].ChannelQueueSize; got != DefaultTaskChannelQueueSize {
		t.Fatalf("unexpected channel_queue_size default: got=%d want=%d", got, DefaultTaskChannelQueueSize)
	}
}

func TestApplyDefaultsSetsSocketBufferDefaults(t *testing.T) {
	cfg := Config{
		Receivers: map[string]ReceiverConfig{"r1": {Type: "udp_gnet", Listen: ":9000"}},
		Senders:   map[string]SenderConfig{"s1": {Type: "udp_unicast", Remote: "127.0.0.1:9001", LocalPort: 9002}},
	}
	cfg.ApplyDefaults(BusinessDefaultsConfig{})

	if got := cfg.Receivers["r1"].SocketRecvBuffer; got != DefaultReceiverSocketRecvBuffer {
		t.Fatalf("unexpected receiver socket_recv_buffer default: %d", got)
	}
	if got := cfg.Senders["s1"].SocketSendBuffer; got != DefaultSenderSocketSendBuffer {
		t.Fatalf("unexpected sender socket_send_buffer default: %d", got)
	}
}

func TestApplyDefaultsPreservesExplicitSocketBufferValues(t *testing.T) {
	cfg := Config{
		Receivers: map[string]ReceiverConfig{"r1": {Type: "udp_gnet", Listen: ":9000", SocketRecvBuffer: 2 << 20}},
		Senders:   map[string]SenderConfig{"s1": {Type: "udp_unicast", Remote: "127.0.0.1:9001", LocalPort: 9002, SocketSendBuffer: 3 << 20}},
	}
	cfg.ApplyDefaults(BusinessDefaultsConfig{})

	if got := cfg.Receivers["r1"].SocketRecvBuffer; got != 2<<20 {
		t.Fatalf("receiver socket_recv_buffer should be preserved: %d", got)
	}
	if got := cfg.Senders["s1"].SocketSendBuffer; got != 3<<20 {
		t.Fatalf("sender socket_send_buffer should be preserved: %d", got)
	}
}
