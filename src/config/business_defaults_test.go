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
			Task:     TaskConfig{PoolSize: 100, ChannelQueueSize: 300, ExecutionModel: "channel", PayloadLogMaxBytes: 111},
			Receiver: ReceiverConfig{Multicore: &m, NumEventLoop: 12, PayloadLogMaxBytes: 222},
			Sender:   SenderConfig{Concurrency: 16},
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

func TestSystemBusinessDefaultsDoNotOverrideExplicitReceiverMulticore(t *testing.T) {
	m := true
	explicitFalse := false
	sys := SystemConfig{
		BusinessDefaults: BusinessDefaultsConfig{
			Receiver: ReceiverConfig{Multicore: &m},
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

func TestBusinessDefaultsReuseBusinessSchemaFields(t *testing.T) {
	dir := t.TempDir()
	systemPath := filepath.Join(dir, "system.json")
	payload := `{
		"logging":{"level":"info"},
		"business_defaults":{
			"receiver":{
				"socket_recv_buffer":12345,
				"dial_timeout":"3s",
				"balancers":["range"]
			},
			"sender":{
				"socket_send_buffer":23456,
				"request_timeout":"4s",
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
	if sys.BusinessDefaults.Receiver.SocketRecvBuffer != 12345 || sys.BusinessDefaults.Receiver.DialTimeout != "3s" {
		t.Fatalf("receiver defaults were not decoded through ReceiverConfig: %+v", sys.BusinessDefaults.Receiver)
	}
	if len(sys.BusinessDefaults.Receiver.Balancers) != 1 || sys.BusinessDefaults.Receiver.Balancers[0] != "range" {
		t.Fatalf("receiver balancers default was not decoded: %#v", sys.BusinessDefaults.Receiver.Balancers)
	}
	if sys.BusinessDefaults.Sender.SocketSendBuffer != 23456 || sys.BusinessDefaults.Sender.RequestTimeout != "4s" || sys.BusinessDefaults.Sender.Partitioner != "round_robin" {
		t.Fatalf("sender defaults were not decoded through SenderConfig: %+v", sys.BusinessDefaults.Sender)
	}
}

func TestMergeDoesNotApplyDefaults(t *testing.T) {
	sys := SystemConfig{
		BusinessDefaults: BusinessDefaultsConfig{
			Task:   TaskConfig{PoolSize: 64},
			Sender: SenderConfig{Concurrency: 4},
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
			Task:     TaskConfig{PoolSize: 64, ChannelQueueSize: 128, PayloadLogMaxBytes: 256},
			Receiver: ReceiverConfig{Multicore: &explicitFalse, NumEventLoop: 12, PayloadLogMaxBytes: 300, SocketRecvBuffer: 400},
			Sender:   SenderConfig{Concurrency: 4, SocketSendBuffer: 500},
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
			Task: TaskConfig{PayloadLogMaxBytes: 777},
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
