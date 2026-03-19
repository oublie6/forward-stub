package config

import (
	"strings"
	"testing"
)

func kafkaSenderBaseConfig() Config {
	cfg := attachMinimalRouting(Config{
		Version: 1,
		Logging: LoggingConfig{Level: "info"},
		Receivers: map[string]ReceiverConfig{
			"r1": {Type: "udp_gnet", Listen: ":19000"},
		},
		Senders: map[string]SenderConfig{
			"k1": {Type: "kafka", Remote: "127.0.0.1:9092", Topic: "out"},
		},
		Pipelines: map[string][]StageConfig{"p1": {}},
		Tasks: map[string]TaskConfig{
			"t1": {Pipelines: []string{"p1"}, Senders: []string{"k1"}},
		},
	})
	cfg.ApplyDefaults()
	return cfg
}

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

func TestValidateKafkaSenderRejectsHashKeyPartitionerWithoutKey(t *testing.T) {
	cfg := kafkaSenderBaseConfig()
	s := cfg.Senders["k1"]
	s.Partitioner = "hash_key"
	cfg.Senders["k1"] = s
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "requires record_key or record_key_source") {
		t.Fatalf("expected partitioner validation error, got: %v", err)
	}
}

func TestValidateKafkaSenderRejectsUnsupportedRecordKeySource(t *testing.T) {
	cfg := kafkaSenderBaseConfig()
	s := cfg.Senders["k1"]
	s.RecordKeySource = "json_path"
	cfg.Senders["k1"] = s
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "record_key_source unsupported") {
		t.Fatalf("expected record_key_source validation error, got: %v", err)
	}
}

func TestValidateKafkaSenderRejectsCompressionLevelWithoutSupportedCodec(t *testing.T) {
	cfg := kafkaSenderBaseConfig()
	s := cfg.Senders["k1"]
	s.Compression = "snappy"
	s.CompressionLevel = 3
	cfg.Senders["k1"] = s
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "compression_level requires compression") {
		t.Fatalf("expected compression level validation error, got: %v", err)
	}
}

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
