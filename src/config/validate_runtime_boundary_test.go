package config

import (
	"strings"
	"testing"
)

func validBoundaryConfig() Config {
	cfg := Config{
		Logging: LoggingConfig{Level: "info"},
		Receivers: map[string]ReceiverConfig{
			"r1": {Type: "udp_gnet", Listen: ":9001", Selector: "sel1"},
		},
		Selectors: map[string]SelectorConfig{"sel1": {DefaultTaskSet: "ts1"}},
		TaskSets:  map[string][]string{"ts1": {"t1"}},
		Senders: map[string]SenderConfig{
			"s1": {Type: "tcp_gnet", Remote: "127.0.0.1:9002"},
		},
		Pipelines: map[string][]StageConfig{"p1": {}},
		Tasks:     map[string]TaskConfig{"t1": {Pipelines: []string{"p1"}, Senders: []string{"s1"}}},
	}
	cfg.ApplyDefaults(BusinessDefaultsConfig{})
	return cfg
}

func TestValidateRejectsBuilderOnlyNetworkErrors(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*Config)
		want string
	}{
		{
			name: "udp receiver missing listen",
			mut: func(cfg *Config) {
				cfg.Receivers["r1"] = ReceiverConfig{Type: "udp_gnet", Selector: "sel1"}
			},
			want: "requires listen",
		},
		{
			name: "tcp receiver invalid frame",
			mut: func(cfg *Config) {
				cfg.Receivers["r1"] = ReceiverConfig{Type: "tcp_gnet", Listen: ":9001", Selector: "sel1", Frame: "bad"}
			},
			want: "frame must be empty or u16be",
		},
		{
			name: "udp sender missing local port",
			mut: func(cfg *Config) {
				cfg.Senders["s1"] = SenderConfig{Type: "udp_unicast", Remote: "127.0.0.1:9002"}
			},
			want: "requires local_port",
		},
		{
			name: "tcp sender missing remote",
			mut: func(cfg *Config) {
				cfg.Senders["s1"] = SenderConfig{Type: "tcp_gnet"}
			},
			want: "requires remote",
		},
		{
			name: "logging interval invalid",
			mut: func(cfg *Config) {
				cfg.Logging.TrafficStatsInterval = "0s"
			},
			want: "traffic_stats_interval",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validBoundaryConfig()
			tt.mut(&cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestValidateRejectsPipelineStageSyntaxBeforeRuntimeBuild(t *testing.T) {
	tests := []struct {
		name  string
		stage StageConfig
		want  string
	}{
		{name: "unknown stage", stage: StageConfig{Type: "nope"}, want: "unknown stage type"},
		{name: "negative offset", stage: StageConfig{Type: "match_offset_bytes", Offset: -1, Hex: "aa"}, want: "offset must be >= 0"},
		{name: "bad hex", stage: StageConfig{Type: "replace_offset_bytes", Offset: 0, Hex: "xx"}, want: "hex invalid"},
		{name: "empty route cases", stage: StageConfig{Type: "route_offset_bytes_sender"}, want: "non-empty cases"},
		{name: "route length mismatch", stage: StageConfig{Type: "route_offset_bytes_sender", Cases: map[string]string{"aa": "s1", "aabb": "s1"}}, want: "length mismatch"},
		{name: "kafka key negative offset", stage: StageConfig{Type: "set_kafka_record_key_from_offset_bytes", Offset: -1, Length: 1, Encoding: "text"}, want: "offset must be >= 0"},
		{name: "kafka key zero length", stage: StageConfig{Type: "set_kafka_record_key_from_offset_bytes", Offset: 0, Encoding: "text"}, want: "length must be > 0"},
		{name: "kafka key bad encoding", stage: StageConfig{Type: "set_kafka_record_key_from_offset_bytes", Offset: 0, Length: 1, Encoding: "base64"}, want: "encoding must be text or hex"},
		{name: "bad regex", stage: StageConfig{Type: "rewrite_target_path_regex", Pattern: "["}, want: "invalid pattern"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validBoundaryConfig()
			cfg.Pipelines["p1"] = []StageConfig{tt.stage}
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got %v", tt.want, err)
			}
		})
	}
}
