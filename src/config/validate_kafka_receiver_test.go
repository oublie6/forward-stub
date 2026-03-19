package config

import (
	"strings"
	"testing"
)

func kafkaReceiverBaseConfig() Config {
	cfg := Config{
		Version: 1,
		Logging: LoggingConfig{Level: "info"},
		Receivers: map[string]ReceiverConfig{
			"r1": {Type: "kafka", Listen: "127.0.0.1:9092", Selector: "sel1", Topic: "in"},
		},
		Selectors: map[string]SelectorConfig{"sel1": {DefaultTaskSet: "ts1"}},
		TaskSets:  map[string][]string{"ts1": {"t1"}},
		Senders:   map[string]SenderConfig{"s1": {Type: "tcp_gnet", Remote: "127.0.0.1:9001"}},
		Pipelines: map[string][]StageConfig{"p1": {}},
		Tasks:     map[string]TaskConfig{"t1": {Pipelines: []string{"p1"}, Senders: []string{"s1"}}},
	}
	cfg.ApplyDefaults()
	return cfg
}

func TestValidateKafkaReceiverRejectsInvalidBalancer(t *testing.T) {
	cfg := kafkaReceiverBaseConfig()
	rc := cfg.Receivers["r1"]
	rc.Balancers = []string{"bad"}
	cfg.Receivers["r1"] = rc
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "balancer unsupported") {
		t.Fatalf("expected balancer validation error, got: %v", err)
	}
}

func TestValidateKafkaReceiverRejectsInvalidIsolationLevel(t *testing.T) {
	cfg := kafkaReceiverBaseConfig()
	rc := cfg.Receivers["r1"]
	rc.IsolationLevel = "serializable"
	cfg.Receivers["r1"] = rc
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "isolation_level unsupported") {
		t.Fatalf("expected isolation level validation error, got: %v", err)
	}
}

func TestValidateKafkaReceiverRejectsAutoCommitIntervalWhenDisabled(t *testing.T) {
	cfg := kafkaReceiverBaseConfig()
	rc := cfg.Receivers["r1"]
	v := false
	rc.AutoCommit = &v
	rc.AutoCommitInterval = "5s"
	cfg.Receivers["r1"] = rc
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "auto_commit_interval requires auto_commit=true") {
		t.Fatalf("expected auto_commit interval validation error, got: %v", err)
	}
}

func TestValidateKafkaReceiverRejectsHeartbeatNotLessThanSession(t *testing.T) {
	cfg := kafkaReceiverBaseConfig()
	rc := cfg.Receivers["r1"]
	rc.SessionTimeout = "3s"
	rc.HeartbeatInterval = "3s"
	cfg.Receivers["r1"] = rc
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "heartbeat_interval must be less than session_timeout") {
		t.Fatalf("expected heartbeat validation error, got: %v", err)
	}
}
