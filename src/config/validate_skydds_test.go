package config

import "testing"

func baseSkyDDSConfig() Config {
	cfg := Config{
		Logging: LoggingConfig{Level: "info", GCStatsLogInterval: "1m"},
		Receivers: map[string]ReceiverConfig{
			"rx": {Type: "dds_skydds", Selector: "sel1", DCPSConfigFile: "./dds.ini", DomainID: 0, TopicName: "TopicA", MessageModel: "octet"},
		},
		Selectors: map[string]SelectorConfig{"sel1": {DefaultTaskSet: "ts1"}},
		TaskSets:  map[string][]string{"ts1": {"t1"}},
		Senders: map[string]SenderConfig{
			"tx": {Type: "dds_skydds", DCPSConfigFile: "./dds.ini", DomainID: 0, TopicName: "TopicB", MessageModel: "octet"},
		},
		Pipelines: map[string][]StageConfig{"p1": {}},
		Tasks:     map[string]TaskConfig{"t1": {Pipelines: []string{"p1"}, Senders: []string{"tx"}, ExecutionModel: "fastpath"}},
	}
	cfg.ApplyDefaults()
	return cfg
}

func TestValidateSkyDDSOK(t *testing.T) {
	cfg := baseSkyDDSConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate failed: %v", err)
	}
}

func TestApplyDefaultsSkyDDSReceiverKnobs(t *testing.T) {
	cfg := baseSkyDDSConfig()
	rx := cfg.Receivers["rx"]
	if rx.WaitTimeout != DefaultSkyDDSWaitTimeout {
		t.Fatalf("wait_timeout default mismatch: got=%q want=%q", rx.WaitTimeout, DefaultSkyDDSWaitTimeout)
	}
	if rx.DrainMaxItems != DefaultSkyDDSDrainMaxItems {
		t.Fatalf("drain_max_items default mismatch: got=%d want=%d", rx.DrainMaxItems, DefaultSkyDDSDrainMaxItems)
	}
}

func TestValidateSkyDDSBatchOK(t *testing.T) {
	cfg := baseSkyDDSConfig()
	rx := cfg.Receivers["rx"]
	rx.MessageModel = "batch_octet"
	cfg.Receivers["rx"] = rx
	tx := cfg.Senders["tx"]
	tx.MessageModel = "batch_octet"
	tx.BatchNum = 16
	tx.BatchSize = 4096
	tx.BatchDelay = "200ms"
	cfg.Senders["tx"] = tx
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate failed: %v", err)
	}
}

func TestValidateSkyDDSBatchMissingParams(t *testing.T) {
	cfg := baseSkyDDSConfig()
	tx := cfg.Senders["tx"]
	tx.MessageModel = "batch_octet"
	cfg.Senders["tx"] = tx
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateSkyDDSMessageModelInvalid(t *testing.T) {
	cfg := baseSkyDDSConfig()
	rx := cfg.Receivers["rx"]
	rx.MessageModel = "abc"
	cfg.Receivers["rx"] = rx
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateSkyDDSReceiverWaitTimeoutInvalid(t *testing.T) {
	cfg := baseSkyDDSConfig()
	rx := cfg.Receivers["rx"]
	rx.WaitTimeout = "abc"
	cfg.Receivers["rx"] = rx
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateSkyDDSReceiverDrainMaxItemsInvalid(t *testing.T) {
	cfg := baseSkyDDSConfig()
	rx := cfg.Receivers["rx"]
	rx.DrainMaxItems = 0
	cfg.Receivers["rx"] = rx
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateSkyDDSReceiverCustomKnobsOK(t *testing.T) {
	cfg := baseSkyDDSConfig()
	rx := cfg.Receivers["rx"]
	rx.WaitTimeout = "5ms"
	rx.DrainMaxItems = 64
	cfg.Receivers["rx"] = rx
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate failed: %v", err)
	}
}
