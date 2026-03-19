package config

import "testing"

func TestApplyDefaultsSetsKafkaSenderOptionDefaults(t *testing.T) {
	cfg := Config{
		Receivers: map[string]ReceiverConfig{"r1": {Type: "udp_gnet", Listen: ":9000", Selector: "sel1"}},
		Selectors: map[string]SelectorConfig{"sel1": {DefaultTaskSet: "ts1"}},
		TaskSets:  map[string][]string{"ts1": {"t1"}},
		Senders:   map[string]SenderConfig{"k1": {Type: "kafka", Remote: "127.0.0.1:9092", Topic: "out"}},
		Pipelines: map[string][]StageConfig{"p1": {}},
		Tasks:     map[string]TaskConfig{"t1": {Pipelines: []string{"p1"}, Senders: []string{"k1"}}},
	}
	cfg.ApplyDefaults()

	got := cfg.Senders["k1"]
	if got.DialTimeout != DefaultKafkaDialTimeout ||
		got.RequestTimeout != DefaultKafkaSenderRequestTimeout ||
		got.RetryTimeout != DefaultKafkaRetryTimeout ||
		got.RetryBackoff != DefaultKafkaRetryBackoff ||
		got.ConnIdleTimeout != DefaultKafkaConnIdleTimeout ||
		got.MetadataMaxAge != DefaultKafkaMetadataMaxAge ||
		got.Partitioner != DefaultKafkaSenderPartitioner {
		t.Fatalf("unexpected kafka sender defaults: %+v", got)
	}
}

func TestApplyDefaultsSetsKafkaReceiverOptionDefaults(t *testing.T) {
	cfg := Config{
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

	got := cfg.Receivers["r1"]
	if got.DialTimeout != DefaultKafkaDialTimeout ||
		got.ConnIdleTimeout != DefaultKafkaConnIdleTimeout ||
		got.MetadataMaxAge != DefaultKafkaMetadataMaxAge ||
		got.RetryBackoff != DefaultKafkaRetryBackoff ||
		got.SessionTimeout != DefaultKafkaReceiverSessionTTL ||
		got.HeartbeatInterval != DefaultKafkaReceiverHeartbeat ||
		got.RebalanceTimeout != DefaultKafkaReceiverRebalanceTTL ||
		got.AutoCommitInterval != DefaultKafkaReceiverAutoCommitIv ||
		got.FetchMaxPartitionBytes != DefaultKafkaFetchMaxPartBytes ||
		got.IsolationLevel != DefaultKafkaIsolationLevel {
		t.Fatalf("unexpected kafka receiver defaults: %+v", got)
	}
	if got.AutoCommit == nil || !*got.AutoCommit {
		t.Fatalf("unexpected kafka receiver auto_commit default: %+v", got.AutoCommit)
	}
	if len(got.Balancers) != 1 || got.Balancers[0] != "cooperative_sticky" {
		t.Fatalf("unexpected kafka receiver balancers default: %#v", got.Balancers)
	}
}
