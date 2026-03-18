package config

import "testing"

// kafkaSenderBaseConfig 是供 validate_kafka_sender_test.go 使用的包内辅助函数。
func kafkaSenderBaseConfig() Config {
	cfg := Config{
		Version: 1,
		Logging: LoggingConfig{Level: "info"},
		Receivers: map[string]ReceiverConfig{
			"r1": {Type: "udp_gnet", Listen: ":19000"},
		},
		Senders: map[string]SenderConfig{
			"k1": {Type: "kafka", Remote: "127.0.0.1:9092", Topic: "out"},
		},
		Pipelines: map[string][]StageConfig{"p1": {}},
		Selectors: testSelectors("r1", "t1"),
		Tasks: map[string]TaskConfig{
			"t1": {Pipelines: []string{"p1"}, Senders: []string{"k1"}},
		},
	}
	cfg.ApplyDefaults()
	return cfg
}

// TestValidateKafkaSenderIdempotentAcksConstraint 验证 config 包中 ValidateKafkaSenderIdempotentAcksConstraint 的行为。
func TestValidateKafkaSenderIdempotentAcksConstraint(t *testing.T) {
	cfg := kafkaSenderBaseConfig()
	v := true
	s := cfg.Senders["k1"]
	s.Idempotent = &v
	s.Acks = "1"
	cfg.Senders["k1"] = s
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error for idempotent=true with acks=1")
	}
}

// TestValidateKafkaSenderIdempotentAllowsConfiguredMaxInFlight 验证 config 包中 ValidateKafkaSenderIdempotentAllowsConfiguredMaxInFlight 的行为。
func TestValidateKafkaSenderIdempotentAllowsConfiguredMaxInFlight(t *testing.T) {
	cfg := kafkaSenderBaseConfig()
	v := true
	s := cfg.Senders["k1"]
	s.Idempotent = &v
	s.MaxInFlightRequestsPerConnection = 2
	cfg.Senders["k1"] = s
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

// TestValidateKafkaSenderNonIdempotentAllowsAcks1 验证 config 包中 ValidateKafkaSenderNonIdempotentAllowsAcks1 的行为。
func TestValidateKafkaSenderNonIdempotentAllowsAcks1(t *testing.T) {
	cfg := kafkaSenderBaseConfig()
	v := false
	s := cfg.Senders["k1"]
	s.Idempotent = &v
	s.Acks = "1"
	s.MaxInFlightRequestsPerConnection = 5
	s.Retries = 3
	cfg.Senders["k1"] = s
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

// TestKafkaAcksConfigUnmarshal 验证 config 包中 KafkaAcksConfigUnmarshal 的行为。
func TestKafkaAcksConfigUnmarshal(t *testing.T) {
	var a KafkaAcksConfig
	if err := a.UnmarshalJSON([]byte("-1")); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if a.Int() != -1 {
		t.Fatalf("expected -1, got %d", a.Int())
	}
	if err := a.UnmarshalJSON([]byte(`"all"`)); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if a.Int() != -1 || !a.IsValid() {
		t.Fatalf("expected valid all/-1 mapping, got %q", string(a))
	}
}
