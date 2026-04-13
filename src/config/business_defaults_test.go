package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSystemBusinessDefaultsAppliedToBusinessConfig(t *testing.T) {
	m := true
	sys := SystemConfig{
		BusinessDefaults: BusinessDefaultsConfig{
			Task:     BusinessTaskDefaultsConfig{PoolSize: 100, ChannelQueueSize: 300, ExecutionModel: "channel", PayloadLogMaxBytes: 111},
			Receiver: BusinessReceiverDefaultsConfig{Multicore: &m, NumEventLoop: 12, PayloadLogMaxBytes: 222},
			Sender:   BusinessSenderDefaultsConfig{Concurrency: 16},
		},
	}
	biz := BusinessConfig{
		Version:   1,
		Receivers: map[string]ReceiverConfig{"r1": {Type: "udp_gnet", Listen: ":9000"}},
		Senders:   map[string]SenderConfig{"s1": {Type: "tcp_gnet", Remote: "127.0.0.1:9100"}},
		Pipelines: map[string][]StageConfig{"p1": {}},
		Tasks:     map[string]TaskConfig{"t1": {Pipelines: []string{"p1"}, Senders: []string{"s1"}}},
	}

	cfg := sys.Merge(biz)
	cfg.ApplyDefaults(sys.BusinessDefaults)

	tc := cfg.Tasks["t1"]
	if tc.PoolSize != 100 || tc.ChannelQueueSize != 300 || tc.ExecutionModel != "channel" || tc.PayloadLogMaxBytes != 111 {
		t.Fatalf("unexpected task defaults: %+v", tc)
	}
	rc := cfg.Receivers["r1"]
	if rc.Multicore == nil || !*rc.Multicore || rc.NumEventLoop != 12 || rc.PayloadLogMaxBytes != 222 {
		t.Fatalf("unexpected receiver defaults: %+v", rc)
	}
	sc := cfg.Senders["s1"]
	if sc.Concurrency != 16 {
		t.Fatalf("unexpected sender defaults: %+v", sc)
	}
}

func TestSystemBusinessDefaultsAppliedToSupportedProtocolFields(t *testing.T) {
	m := false
	autoCommit := true
	sys := SystemConfig{
		BusinessDefaults: BusinessDefaultsConfig{
			Receiver: BusinessReceiverDefaultsConfig{
				Multicore:              &m,
				NumEventLoop:           3,
				SocketRecvBuffer:       1234,
				PayloadLogMaxBytes:     77,
				DialTimeout:            "1s",
				ConnIdleTimeout:        "2s",
				MetadataMaxAge:         "3s",
				RetryBackoff:           "4s",
				SessionTimeout:         "5s",
				HeartbeatInterval:      "6s",
				RebalanceTimeout:       "7s",
				Balancers:              []string{"range"},
				AutoCommit:             &autoCommit,
				AutoCommitInterval:     "8s",
				FetchMaxPartitionBytes: 2048,
				IsolationLevel:         "read_committed",
				WaitTimeout:            "9s",
				DrainMaxItems:          10,
				DrainBufferBytes:       11,
			},
			Sender: BusinessSenderDefaultsConfig{
				Concurrency:      12,
				SocketSendBuffer: 3456,
				DialTimeout:      "13s",
				RequestTimeout:   "14s",
				RetryTimeout:     "15s",
				RetryBackoff:     "16s",
				ConnIdleTimeout:  "17s",
				MetadataMaxAge:   "18s",
				Partitioner:      "round_robin",
			},
		},
	}
	biz := BusinessConfig{
		Version: 1,
		Receivers: map[string]ReceiverConfig{
			"rk": {Type: "kafka", Listen: "127.0.0.1:9092", Topic: "in"},
			"rd": {Type: "dds_skydds"},
		},
		Senders: map[string]SenderConfig{
			"sk": {Type: "kafka", Remote: "127.0.0.1:9092", Topic: "out"},
		},
	}

	cfg := sys.Merge(biz)
	cfg.ApplyDefaults(sys.BusinessDefaults)

	rk := cfg.Receivers["rk"]
	if rk.Multicore == nil || *rk.Multicore || rk.NumEventLoop != 3 || rk.SocketRecvBuffer != 1234 || rk.PayloadLogMaxBytes != 77 {
		t.Fatalf("unexpected kafka receiver common defaults: %+v", rk)
	}
	if rk.DialTimeout != "1s" || rk.ConnIdleTimeout != "2s" || rk.MetadataMaxAge != "3s" ||
		rk.RetryBackoff != "4s" || rk.SessionTimeout != "5s" || rk.HeartbeatInterval != "6s" ||
		rk.RebalanceTimeout != "7s" || len(rk.Balancers) != 1 || rk.Balancers[0] != "range" ||
		rk.AutoCommit == nil || !*rk.AutoCommit || rk.AutoCommitInterval != "8s" ||
		rk.FetchMaxPartitionBytes != 2048 || rk.IsolationLevel != "read_committed" {
		t.Fatalf("unexpected kafka receiver protocol defaults: %+v", rk)
	}

	rd := cfg.Receivers["rd"]
	if rd.WaitTimeout != "9s" || rd.DrainMaxItems != 10 || rd.DrainBufferBytes != 11 {
		t.Fatalf("unexpected skydds receiver defaults: %+v", rd)
	}

	sk := cfg.Senders["sk"]
	if sk.Concurrency != 12 || sk.SocketSendBuffer != 3456 || sk.DialTimeout != "13s" ||
		sk.RequestTimeout != "14s" || sk.RetryTimeout != "15s" || sk.RetryBackoff != "16s" ||
		sk.ConnIdleTimeout != "17s" || sk.MetadataMaxAge != "18s" || sk.Partitioner != "round_robin" {
		t.Fatalf("unexpected kafka sender defaults: %+v", sk)
	}
}

func TestSystemBusinessDefaultsDoNotOverrideExplicitReceiverMulticore(t *testing.T) {
	m := true
	explicitFalse := false
	sys := SystemConfig{
		BusinessDefaults: BusinessDefaultsConfig{
			Receiver: BusinessReceiverDefaultsConfig{Multicore: &m},
		},
	}
	biz := BusinessConfig{
		Version:   1,
		Receivers: map[string]ReceiverConfig{"r1": {Type: "udp_gnet", Listen: ":9000", Multicore: &explicitFalse}},
		Senders:   map[string]SenderConfig{"s1": {Type: "tcp_gnet", Remote: "127.0.0.1:9100"}},
		Pipelines: map[string][]StageConfig{"p1": {}},
		Tasks:     map[string]TaskConfig{"t1": {Pipelines: []string{"p1"}, Senders: []string{"s1"}}},
	}

	cfg := sys.Merge(biz)
	cfg.ApplyDefaults(sys.BusinessDefaults)

	rc := cfg.Receivers["r1"]
	if rc.Multicore == nil || *rc.Multicore {
		t.Fatalf("explicit multicore=false should be preserved, got %+v", rc.Multicore)
	}
}

func TestBusinessDefaultsAcceptOnlySupportedSchemaFields(t *testing.T) {
	dir := t.TempDir()
	systemPath := filepath.Join(dir, "system.json")
	autoCommit := false
	payload := `{
		"logging":{"level":"info"},
		"business_defaults":{
			"task":{
				"execution_model":"channel",
				"pool_size":32,
				"channel_queue_size":64,
				"payload_log_max_bytes":128
			},
			"receiver":{
				"socket_recv_buffer":12345,
				"dial_timeout":"3s",
				"conn_idle_timeout":"4s",
				"metadata_max_age":"5s",
				"retry_backoff":"6s",
				"session_timeout":"7s",
				"heartbeat_interval":"8s",
				"rebalance_timeout":"9s",
				"balancers":["range"],
				"auto_commit":false,
				"auto_commit_interval":"10s",
				"fetch_max_partition_bytes":4096,
				"isolation_level":"read_committed",
				"wait_timeout":"11s",
				"drain_max_items":12,
				"drain_buffer_bytes":13
			},
			"sender":{
				"socket_send_buffer":23456,
				"request_timeout":"4s",
				"dial_timeout":"5s",
				"retry_timeout":"6s",
				"retry_backoff":"7s",
				"conn_idle_timeout":"8s",
				"metadata_max_age":"9s",
				"partitioner":"round_robin"
			}
		}
	}`
	if err := os.WriteFile(systemPath, []byte(payload), 0o644); err != nil {
		t.Fatalf("write system config: %v", err)
	}

	sys, err := LoadSystemLocal(systemPath)
	if err != nil {
		t.Fatalf("load system config: %v", err)
	}
	if sys.BusinessDefaults.Task.ExecutionModel != "channel" || sys.BusinessDefaults.Task.PoolSize != 32 ||
		sys.BusinessDefaults.Task.ChannelQueueSize != 64 || sys.BusinessDefaults.Task.PayloadLogMaxBytes != 128 {
		t.Fatalf("task defaults were not decoded through narrow defaults schema: %+v", sys.BusinessDefaults.Task)
	}
	if sys.BusinessDefaults.Receiver.SocketRecvBuffer != 12345 || sys.BusinessDefaults.Receiver.DialTimeout != "3s" {
		t.Fatalf("receiver defaults were not decoded through narrow defaults schema: %+v", sys.BusinessDefaults.Receiver)
	}
	if len(sys.BusinessDefaults.Receiver.Balancers) != 1 || sys.BusinessDefaults.Receiver.Balancers[0] != "range" {
		t.Fatalf("receiver balancers default was not decoded: %#v", sys.BusinessDefaults.Receiver.Balancers)
	}
	if sys.BusinessDefaults.Receiver.AutoCommit == nil || *sys.BusinessDefaults.Receiver.AutoCommit != autoCommit {
		t.Fatalf("receiver auto_commit default was not decoded: %#v", sys.BusinessDefaults.Receiver.AutoCommit)
	}
	if sys.BusinessDefaults.Receiver.WaitTimeout != "11s" || sys.BusinessDefaults.Receiver.DrainMaxItems != 12 ||
		sys.BusinessDefaults.Receiver.DrainBufferBytes != 13 {
		t.Fatalf("receiver skydds defaults were not decoded: %+v", sys.BusinessDefaults.Receiver)
	}
	if sys.BusinessDefaults.Sender.SocketSendBuffer != 23456 || sys.BusinessDefaults.Sender.RequestTimeout != "4s" || sys.BusinessDefaults.Sender.Partitioner != "round_robin" {
		t.Fatalf("sender defaults were not decoded through narrow defaults schema: %+v", sys.BusinessDefaults.Sender)
	}
}

func TestBusinessDefaultsRejectUnsupportedBusinessSchemaFields(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name    string
		payload string
	}{
		{
			name:    "task topology field",
			payload: `{"logging":{"level":"info"},"business_defaults":{"task":{"senders":["s1"]}}}`,
		},
		{
			name:    "receiver identity field",
			payload: `{"logging":{"level":"info"},"business_defaults":{"receiver":{"type":"kafka"}}}`,
		},
		{
			name:    "receiver auth field",
			payload: `{"logging":{"level":"info"},"business_defaults":{"receiver":{"username":"alice"}}}`,
		},
		{
			name:    "sender protocol main field",
			payload: `{"logging":{"level":"info"},"business_defaults":{"sender":{"topic":"out"}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			systemPath := filepath.Join(dir, tt.name+".json")
			if err := os.WriteFile(systemPath, []byte(tt.payload), 0o644); err != nil {
				t.Fatalf("write system config: %v", err)
			}
			if _, err := LoadSystemLocal(systemPath); err == nil {
				t.Fatalf("expected unsupported business_defaults field to be rejected")
			}
		})
	}
}

func TestMergeDoesNotApplyDefaults(t *testing.T) {
	sys := SystemConfig{
		BusinessDefaults: BusinessDefaultsConfig{
			Task:   BusinessTaskDefaultsConfig{PoolSize: 64},
			Sender: BusinessSenderDefaultsConfig{Concurrency: 4},
		},
	}
	biz := BusinessConfig{
		Version:   1,
		Receivers: map[string]ReceiverConfig{"r1": {Type: "udp_gnet", Listen: ":9000"}},
		Senders:   map[string]SenderConfig{"s1": {Type: "tcp_gnet", Remote: "127.0.0.1:9100"}},
		Pipelines: map[string][]StageConfig{"p1": {}},
		Tasks:     map[string]TaskConfig{"t1": {Pipelines: []string{"p1"}, Senders: []string{"s1"}}},
	}

	cfg := sys.Merge(biz)
	if cfg.Tasks["t1"].PoolSize != 0 {
		t.Fatalf("Merge should not apply task defaults: %+v", cfg.Tasks["t1"])
	}
	if cfg.Senders["s1"].Concurrency != 0 {
		t.Fatalf("Merge should not apply sender defaults: %+v", cfg.Senders["s1"])
	}
	if cfg.Control.TimeoutSec != 0 || cfg.Logging.Level != "" {
		t.Fatalf("Merge should not apply system defaults: control=%+v logging=%+v", cfg.Control, cfg.Logging)
	}
}

func TestBusinessExplicitBeatsSystemDefaultsAndSystemDefaultsBeatCodeDefaults(t *testing.T) {
	explicitFalse := false
	sys := SystemConfig{
		Logging: LoggingConfig{PayloadLogMaxBytes: 900},
		BusinessDefaults: BusinessDefaultsConfig{
			Task:     BusinessTaskDefaultsConfig{PoolSize: 64, ChannelQueueSize: 128, PayloadLogMaxBytes: 256},
			Receiver: BusinessReceiverDefaultsConfig{Multicore: &explicitFalse, NumEventLoop: 12, PayloadLogMaxBytes: 300, SocketRecvBuffer: 400},
			Sender:   BusinessSenderDefaultsConfig{Concurrency: 4, SocketSendBuffer: 500},
		},
	}
	biz := BusinessConfig{
		Version:   1,
		Receivers: map[string]ReceiverConfig{"r1": {Type: "udp_gnet", Listen: ":9000", Multicore: boolPtr(true), PayloadLogMaxBytes: 301}},
		Senders:   map[string]SenderConfig{"s1": {Type: "tcp_gnet", Remote: "127.0.0.1:9100", Concurrency: 8}},
		Pipelines: map[string][]StageConfig{"p1": {}},
		Tasks:     map[string]TaskConfig{"t1": {PoolSize: 32, Pipelines: []string{"p1"}, Senders: []string{"s1"}}},
	}

	cfg := sys.Merge(biz)
	cfg.ApplyDefaults(sys.BusinessDefaults)

	if got := cfg.Tasks["t1"].PoolSize; got != 32 {
		t.Fatalf("business explicit task pool_size should win: %d", got)
	}
	if got := cfg.Tasks["t1"].ChannelQueueSize; got != 128 {
		t.Fatalf("system business_defaults task channel_queue_size should beat code default: %d", got)
	}
	if got := cfg.Tasks["t1"].PayloadLogMaxBytes; got != 256 {
		t.Fatalf("system business_defaults task payload_log_max_bytes should beat logging default: %d", got)
	}
	rc := cfg.Receivers["r1"]
	if rc.Multicore == nil || !*rc.Multicore {
		t.Fatalf("business explicit receiver multicore should win: %+v", rc.Multicore)
	}
	if rc.NumEventLoop != 12 || rc.PayloadLogMaxBytes != 301 || rc.SocketRecvBuffer != 400 {
		t.Fatalf("unexpected receiver defaults: %+v", rc)
	}
	sc := cfg.Senders["s1"]
	if sc.Concurrency != 8 || sc.SocketSendBuffer != 500 {
		t.Fatalf("unexpected sender defaults: %+v", sc)
	}
}

func TestSystemDefaultsIgnoreBusinessDefaults(t *testing.T) {
	sys := SystemConfig{
		BusinessDefaults: BusinessDefaultsConfig{
			Task: BusinessTaskDefaultsConfig{PayloadLogMaxBytes: 777},
		},
	}
	cfg := sys.Merge(BusinessConfig{})
	cfg.ApplyDefaults(sys.BusinessDefaults)

	if cfg.Logging.PayloadLogMaxBytes != DefaultPayloadLogMaxBytes {
		t.Fatalf("logging payload_log_max_bytes should use code default only, got %d", cfg.Logging.PayloadLogMaxBytes)
	}
	if cfg.Control.TimeoutSec != DefaultControlTimeoutSec {
		t.Fatalf("control timeout should use code default only, got %d", cfg.Control.TimeoutSec)
	}
}

func TestApplyDefaultsProducesCompleteNormalizedConfig(t *testing.T) {
	sys := SystemConfig{}
	biz := BusinessConfig{
		Version:   1,
		Receivers: map[string]ReceiverConfig{"r1": {Type: "kafka", Listen: "127.0.0.1:9092", Topic: "in"}},
		Senders:   map[string]SenderConfig{"s1": {Type: "kafka", Remote: "127.0.0.1:9092", Topic: "out"}},
		Pipelines: map[string][]StageConfig{"p1": {}},
		Tasks:     map[string]TaskConfig{"t1": {Pipelines: []string{"p1"}, Senders: []string{"s1"}}},
	}

	cfg := sys.Merge(biz)
	cfg.ApplyDefaults(sys.BusinessDefaults)

	if cfg.Control.TimeoutSec != DefaultControlTimeoutSec || cfg.Logging.Level != DefaultLogLevel {
		t.Fatalf("system defaults missing: control=%+v logging=%+v", cfg.Control, cfg.Logging)
	}
	if cfg.Tasks["t1"].PoolSize != DefaultTaskPoolSize || cfg.Tasks["t1"].ChannelQueueSize != DefaultTaskChannelQueueSize {
		t.Fatalf("task defaults missing: %+v", cfg.Tasks["t1"])
	}
	rc := cfg.Receivers["r1"]
	if rc.Multicore == nil || !*rc.Multicore || rc.NumEventLoop != max(DefaultReceiverNumEventLoop, runtime.NumCPU()) ||
		rc.SocketRecvBuffer != DefaultReceiverSocketRecvBuffer || rc.DialTimeout != DefaultKafkaDialTimeout ||
		rc.FetchMaxPartitionBytes != DefaultKafkaFetchMaxPartBytes {
		t.Fatalf("receiver defaults missing: %+v", rc)
	}
	sc := cfg.Senders["s1"]
	if sc.Concurrency != DefaultSenderConcurrency || sc.SocketSendBuffer != DefaultSenderSocketSendBuffer ||
		sc.DialTimeout != DefaultKafkaDialTimeout || sc.Partitioner != DefaultKafkaSenderPartitioner {
		t.Fatalf("sender defaults missing: %+v", sc)
	}
}

func boolPtr(v bool) *bool {
	return &v
}
