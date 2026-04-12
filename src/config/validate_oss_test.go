package config

import (
	"strings"
	"testing"
)

func ossReceiverBaseConfig() Config {
	cfg := Config{
		Receivers: map[string]ReceiverConfig{
			"oss": {
				Type:      "oss",
				Selector:  "sel1",
				Endpoint:  "minio.example.com:9000",
				Bucket:    "in",
				AccessKey: "ak",
				SecretKey: "sk",
			},
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

func TestValidateOSSReceiverAllowsDefaultChunkSize(t *testing.T) {
	cfg := ossReceiverBaseConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected omitted oss chunk_size valid: %v", err)
	}
}

func TestValidateOSSReceiverAllowsZeroChunkSize(t *testing.T) {
	cfg := ossReceiverBaseConfig()
	rc := cfg.Receivers["oss"]
	rc.ChunkSize = 0
	cfg.Receivers["oss"] = rc
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected zero oss chunk_size valid: %v", err)
	}
}

func TestValidateOSSReceiverRejectsNegativeChunkSize(t *testing.T) {
	cfg := ossReceiverBaseConfig()
	rc := cfg.Receivers["oss"]
	rc.ChunkSize = -1
	cfg.Receivers["oss"] = rc
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "chunk_size") {
		t.Fatalf("expected negative chunk_size error, got: %v", err)
	}
}
