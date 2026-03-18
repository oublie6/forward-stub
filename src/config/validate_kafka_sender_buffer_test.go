package config

import "testing"

// TestValidateKafkaSenderBufferedLimitsAllowZero 验证 config 包中 ValidateKafkaSenderBufferedLimitsAllowZero 的行为。
func TestValidateKafkaSenderBufferedLimitsAllowZero(t *testing.T) {
	cfg := kafkaSenderBaseConfig()
	s := cfg.Senders["k1"]
	s.MaxBufferedBytes = 0
	s.MaxBufferedRecords = 0
	cfg.Senders["k1"] = s
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

// TestValidateKafkaSenderBufferedLimitsRejectNegative 验证 config 包中 ValidateKafkaSenderBufferedLimitsRejectNegative 的行为。
func TestValidateKafkaSenderBufferedLimitsRejectNegative(t *testing.T) {
	cfg := kafkaSenderBaseConfig()
	s := cfg.Senders["k1"]
	s.MaxBufferedBytes = -1
	cfg.Senders["k1"] = s
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error for negative max_buffered_bytes")
	}

	cfg = kafkaSenderBaseConfig()
	s = cfg.Senders["k1"]
	s.MaxBufferedRecords = -1
	cfg.Senders["k1"] = s
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error for negative max_buffered_records")
	}
}
