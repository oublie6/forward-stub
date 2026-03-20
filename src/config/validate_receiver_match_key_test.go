package config

import (
	"strings"
	"testing"
)

func receiverMatchKeyBaseConfig() Config {
	cfg := Config{
		Receivers: map[string]ReceiverConfig{
			"r1": {Type: "udp_gnet", Listen: ":9000", Selector: "sel1"},
		},
		Tasks: map[string]TaskConfig{
			"t1": {Pipelines: []string{"p1"}, Senders: []string{"s1"}},
		},
		Senders: map[string]SenderConfig{
			"s1": {Type: "tcp_gnet", Remote: "127.0.0.1:9001"},
		},
		Pipelines: map[string][]StageConfig{
			"p1": {},
		},
	}
	return attachMinimalRouting(cfg)
}

func TestValidateReceiverMatchKeyAllowsLegacyDefault(t *testing.T) {
	cfg := receiverMatchKeyBaseConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected legacy default config valid: %v", err)
	}
}

func TestValidateReceiverMatchKeyRejectsUnsupportedMode(t *testing.T) {
	cfg := receiverMatchKeyBaseConfig()
	rc := cfg.Receivers["r1"]
	rc.MatchKey = ReceiverMatchKeyConfig{Mode: "topic"}
	cfg.Receivers["r1"] = rc

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "match_key.mode") {
		t.Fatalf("expected unsupported mode error, got: %v", err)
	}
}

func TestValidateReceiverMatchKeyRejectsMissingFixedValue(t *testing.T) {
	cfg := receiverMatchKeyBaseConfig()
	rc := cfg.Receivers["r1"]
	rc.MatchKey = ReceiverMatchKeyConfig{Mode: "fixed"}
	cfg.Receivers["r1"] = rc

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "fixed_value") {
		t.Fatalf("expected fixed_value error, got: %v", err)
	}
}

func TestValidateReceiverMatchKeyRejectsFixedValueWithNonFixedMode(t *testing.T) {
	cfg := receiverMatchKeyBaseConfig()
	rc := cfg.Receivers["r1"]
	rc.MatchKey = ReceiverMatchKeyConfig{Mode: "remote_ip", FixedValue: "x"}
	cfg.Receivers["r1"] = rc

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "仅可在 mode=fixed 时配置") {
		t.Fatalf("expected non-fixed fixed_value error, got: %v", err)
	}
}

func TestValidateReceiverMatchKeyAcceptsPerReceiverSupportedModes(t *testing.T) {
	cfg := receiverMatchKeyBaseConfig()
	cfg.Receivers = map[string]ReceiverConfig{
		"udp":   {Type: "udp_gnet", Listen: ":9000", Selector: "sel1", MatchKey: ReceiverMatchKeyConfig{Mode: "local_ip"}},
		"tcp":   {Type: "tcp_gnet", Listen: ":9001", Selector: "sel1", MatchKey: ReceiverMatchKeyConfig{Mode: "local_port"}},
		"kafka": {Type: "kafka", Listen: "127.0.0.1:9092", Selector: "sel1", Topic: "orders", Balancers: DefaultKafkaReceiverBalancers, MatchKey: ReceiverMatchKeyConfig{Mode: "topic_partition"}},
		"sftp":  {Type: "sftp", Listen: "127.0.0.1:22", Selector: "sel1", Username: "u", Password: "p", RemoteDir: "/input", HostKeyFingerprint: "SHA256:W5M5Qf3jQ8jD8I2LqzY9zT6QfPj1O9g3k8xw0Jm9r3A", MatchKey: ReceiverMatchKeyConfig{Mode: "filename"}},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected supported modes valid: %v", err)
	}
}
