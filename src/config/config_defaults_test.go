package config

import "testing"

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

func TestApplyDefaultsNormalizesNegativePprofPort(t *testing.T) {
	cfg := Config{Control: ControlConfig{PprofPort: -1}}
	cfg.ApplyDefaults()
	if cfg.Control.PprofPort != 0 {
		t.Fatalf("unexpected pprof_port after defaults: %d", cfg.Control.PprofPort)
	}
}

func TestApplyDefaultsProtocolAwareRuntimeDefaults(t *testing.T) {
	cfg := Config{
		Receivers: map[string]ReceiverConfig{
			"r_udp":   {Type: "udp_gnet", Listen: "127.0.0.1:9001"},
			"r_tcp":   {Type: "tcp_gnet", Listen: "127.0.0.1:9002"},
			"r_kafka": {Type: "kafka", Listen: "127.0.0.1:9092", Topic: "in"},
			"r_sftp":  {Type: "sftp", Listen: "127.0.0.1:22", Username: "u", Password: "p", RemoteDir: "/in", HostKeyFingerprint: "SHA256:W5M5Qf3jQ8jD8I2LqzY9zT6QfPj1O9g3k8xw0Jm9r3A"},
		},
		Senders: map[string]SenderConfig{
			"s_udp":   {Type: "udp_unicast", Remote: "127.0.0.1:9101"},
			"s_tcp":   {Type: "tcp_gnet", Remote: "127.0.0.1:9102"},
			"s_kafka": {Type: "kafka", Remote: "127.0.0.1:9092", Topic: "out"},
			"s_sftp":  {Type: "sftp", Remote: "127.0.0.1:22", Username: "u", Password: "p", RemoteDir: "/out", HostKeyFingerprint: "SHA256:W5M5Qf3jQ8jD8I2LqzY9zT6QfPj1O9g3k8xw0Jm9r3A"},
		},
		Pipelines: map[string][]StageConfig{"p": {}},
		Tasks: map[string]TaskConfig{
			"t_udp":   {Receivers: []string{"r_udp"}, Pipelines: []string{"p"}, Senders: []string{"s_udp"}},
			"t_tcp":   {Receivers: []string{"r_tcp"}, Pipelines: []string{"p"}, Senders: []string{"s_tcp"}},
			"t_kafka": {Receivers: []string{"r_kafka"}, Pipelines: []string{"p"}, Senders: []string{"s_kafka"}},
			"t_sftp":  {Receivers: []string{"r_sftp"}, Pipelines: []string{"p"}, Senders: []string{"s_sftp"}},
		},
	}

	cfg.ApplyDefaults()

	if got := cfg.Tasks["t_udp"].ExecutionModel; got != "fastpath" {
		t.Fatalf("unexpected udp execution model: %s", got)
	}
	if got := cfg.Tasks["t_tcp"].ExecutionModel; got != "fastpath" {
		t.Fatalf("unexpected tcp execution model: %s", got)
	}
	if got := cfg.Tasks["t_kafka"].ExecutionModel; got != "pool" {
		t.Fatalf("unexpected kafka execution model: %s", got)
	}
	if got := cfg.Tasks["t_sftp"].ExecutionModel; got != "pool" {
		t.Fatalf("unexpected sftp execution model: %s", got)
	}

	if cfg.Receivers["r_udp"].Multicore {
		t.Fatalf("unexpected udp receiver multicore default: true")
	}
	if !cfg.Receivers["r_tcp"].Multicore {
		t.Fatalf("unexpected tcp receiver multicore default: false")
	}

	if cfg.Senders["s_udp"].Concurrency <= 0 || cfg.Senders["s_tcp"].Concurrency <= 0 || cfg.Senders["s_kafka"].Concurrency <= 0 || cfg.Senders["s_sftp"].Concurrency <= 0 {
		t.Fatalf("expected protocol sender defaults > 0, got udp=%d tcp=%d kafka=%d sftp=%d",
			cfg.Senders["s_udp"].Concurrency,
			cfg.Senders["s_tcp"].Concurrency,
			cfg.Senders["s_kafka"].Concurrency,
			cfg.Senders["s_sftp"].Concurrency,
		)
	}
}
