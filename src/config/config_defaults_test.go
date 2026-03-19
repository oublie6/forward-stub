package config

import "testing"

// TestApplyDefaultsSetsControlDefaults 验证 config 包中 ApplyDefaultsSetsControlDefaults 的行为。
func TestApplyDefaultsSetsControlDefaults(t *testing.T) {
	cfg := Config{}
	cfg.ApplyDefaults()
	if cfg.Control.ConfigWatchInterval != DefaultConfigWatchInterval {
		t.Fatalf("unexpected config_watch_interval default: %q", cfg.Control.ConfigWatchInterval)
	}
	if cfg.Control.PprofPort != DefaultPprofPort {
		t.Fatalf("unexpected pprof_port default: %d", cfg.Control.PprofPort)
	}
}

// TestApplyDefaultsKeepsDisabledPprofPort 验证 config 包中 ApplyDefaultsKeepsDisabledPprofPort 的行为。
func TestApplyDefaultsKeepsDisabledPprofPort(t *testing.T) {
	cfg := Config{Control: ControlConfig{PprofPort: -1}}
	cfg.ApplyDefaults()
	if cfg.Control.PprofPort != -1 {
		t.Fatalf("unexpected pprof_port after defaults: %d", cfg.Control.PprofPort)
	}
}

// TestApplyDefaultsSetsGCStatsDefaults 验证 config 包中 ApplyDefaultsSetsGCStatsDefaults 的行为。
func TestApplyDefaultsSetsGCStatsDefaults(t *testing.T) {
	cfg := Config{}
	cfg.ApplyDefaults()
	if cfg.Logging.GCStatsEnabled != DefaultGCStatsEnabled {
		t.Fatalf("unexpected gc_stats_enabled default: %v", cfg.Logging.GCStatsEnabled)
	}
	if cfg.Logging.GCStatsInterval != DefaultGCStatsInterval {
		t.Fatalf("unexpected gc_stats_interval default: %q", cfg.Logging.GCStatsInterval)
	}
}

// TestApplyDefaultsSetsReceiverMulticoreWhenUnset 验证 config 包中 ApplyDefaultsSetsReceiverMulticoreWhenUnset 的行为。
func TestApplyDefaultsSetsReceiverMulticoreWhenUnset(t *testing.T) {
	cfg := Config{Receivers: map[string]ReceiverConfig{"r1": {Type: "udp_gnet", Listen: ":9000"}}}
	cfg.ApplyDefaults()
	rc := cfg.Receivers["r1"]
	if rc.Multicore == nil || !*rc.Multicore {
		t.Fatalf("unexpected receiver multicore default: %+v", rc.Multicore)
	}
}

// TestApplyDefaultsSetsSocketBufferDefaults 验证 config 包中 ApplyDefaultsSetsSocketBufferDefaults 的行为。
func TestApplyDefaultsSetsSocketBufferDefaults(t *testing.T) {
	cfg := Config{
		Receivers: map[string]ReceiverConfig{"r1": {Type: "udp_gnet", Listen: ":9000"}},
		Senders:   map[string]SenderConfig{"s1": {Type: "udp_unicast", Remote: "127.0.0.1:9001", LocalPort: 9002}},
	}
	cfg.ApplyDefaults()

	if got := cfg.Receivers["r1"].SocketRecvBuffer; got != DefaultReceiverSocketRecvBuffer {
		t.Fatalf("unexpected receiver socket_recv_buffer default: %d", got)
	}
	if got := cfg.Senders["s1"].SocketSendBuffer; got != DefaultSenderSocketSendBuffer {
		t.Fatalf("unexpected sender socket_send_buffer default: %d", got)
	}
}

// TestApplyDefaultsPreservesExplicitSocketBufferValues 验证 config 包中 ApplyDefaultsPreservesExplicitSocketBufferValues 的行为。
func TestApplyDefaultsPreservesExplicitSocketBufferValues(t *testing.T) {
	cfg := Config{
		Receivers: map[string]ReceiverConfig{"r1": {Type: "udp_gnet", Listen: ":9000", SocketRecvBuffer: 2 << 20}},
		Senders:   map[string]SenderConfig{"s1": {Type: "udp_unicast", Remote: "127.0.0.1:9001", LocalPort: 9002, SocketSendBuffer: 3 << 20}},
	}
	cfg.ApplyDefaults()

	if got := cfg.Receivers["r1"].SocketRecvBuffer; got != 2<<20 {
		t.Fatalf("receiver socket_recv_buffer should be preserved: %d", got)
	}
	if got := cfg.Senders["s1"].SocketSendBuffer; got != 3<<20 {
		t.Fatalf("sender socket_send_buffer should be preserved: %d", got)
	}
}
