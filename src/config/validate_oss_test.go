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

func ossSenderBaseConfig() Config {
	cfg := attachMinimalRouting(Config{
		Receivers: map[string]ReceiverConfig{
			"r1": {Type: "udp_gnet", Listen: ":19000"},
		},
		Senders: map[string]SenderConfig{
			"oss": {
				Type:      "oss",
				Endpoint:  "minio.example.com:9000",
				Bucket:    "out",
				AccessKey: "ak",
				SecretKey: "sk",
			},
		},
		Pipelines: map[string][]StageConfig{"p1": {}},
		Tasks: map[string]TaskConfig{
			"t1": {Pipelines: []string{"p1"}, Senders: []string{"oss"}},
		},
	})
	return cfg
}

func TestApplyDefaultsSetsOSSSenderPartSizeBeforeValidate(t *testing.T) {
	cfg := ossSenderBaseConfig()
	cfg.ApplyDefaults(BusinessDefaultsConfig{})

	if got := cfg.Senders["oss"].PartSize; got != DefaultOSSPartSize {
		t.Fatalf("unexpected oss part_size default: got=%d want=%d", got, DefaultOSSPartSize)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("defaulted oss sender should validate: %v", err)
	}
}

func TestValidateOSSSenderRejectsMissingPartSizeWithoutDefaults(t *testing.T) {
	cfg := ossSenderBaseConfig()
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "part_size") {
		t.Fatalf("expected missing part_size error before defaults, got: %v", err)
	}
}

func TestValidateOSSSenderNotifyOnSuccessKafkaAndSkyDDS(t *testing.T) {
	cfg := ossSenderBaseConfig()
	s := cfg.Senders["oss"]
	s.PartSize = DefaultOSSPartSize
	s.NotifyOnSuccess = NotifyOnSuccessConfigs{
		{Type: "kafka", Remote: "127.0.0.1:9092", Topic: "file-ready", RecordKeySource: "fetch_path"},
		{Type: "dds_skydds", DCPSConfigFile: "dds.ini", DomainID: 0, TopicName: "FileReady", MessageModel: "octet"},
	}
	cfg.Senders["oss"] = s

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected kafka + skydds notify configs valid: %v", err)
	}

	s.NotifyOnSuccess[0].RecordKeySource = "payload"
	cfg.Senders["oss"] = s
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "record_key_source unsupported") {
		t.Fatalf("expected unsupported notify record_key_source error, got: %v", err)
	}
}
