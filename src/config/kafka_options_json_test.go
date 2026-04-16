package config

import (
	"encoding/json"
	"testing"
)

func TestSenderConfigKafkaOptionFieldsUnmarshal(t *testing.T) {
	var sc SenderConfig
	if err := json.Unmarshal([]byte(`{"type":"kafka","remote":"127.0.0.1:9092","topic":"out","dial_timeout":"11s","request_timeout":"31s","retry_timeout":"2m","retry_backoff":"300ms","conn_idle_timeout":"40s","metadata_max_age":"6m","partitioner":"hash_key","record_key_source":"match_key","compression_level":5,"send_mode":"sync","async_queue_size":1024,"async_backpressure":"error","close_flush_timeout":"3s"}`), &sc); err != nil {
		t.Fatalf("unmarshal sender config: %v", err)
	}
	if sc.DialTimeout != "11s" || sc.RequestTimeout != "31s" || sc.Partitioner != "hash_key" || sc.RecordKeySource != "match_key" || sc.CompressionLevel != 5 ||
		sc.SendMode != "sync" || sc.AsyncQueueSize != 1024 || sc.AsyncBackpressure != "error" || sc.CloseFlushTimeout != "3s" {
		t.Fatalf("unexpected sender kafka fields: %+v", sc)
	}
}

func TestReceiverConfigKafkaOptionFieldsUnmarshal(t *testing.T) {
	var rc ReceiverConfig
	if err := json.Unmarshal([]byte(`{"type":"kafka","listen":"127.0.0.1:9092","topic":"in","dial_timeout":"11s","conn_idle_timeout":"40s","metadata_max_age":"6m","retry_backoff":"300ms","session_timeout":"55s","heartbeat_interval":"4s","rebalance_timeout":"70s","balancers":["range","cooperative_sticky"],"auto_commit":false,"fetch_max_partition_bytes":1048576,"isolation_level":"read_committed"}`), &rc); err != nil {
		t.Fatalf("unmarshal receiver config: %v", err)
	}
	if rc.DialTimeout != "11s" || rc.SessionTimeout != "55s" || len(rc.Balancers) != 2 || rc.AutoCommit == nil || *rc.AutoCommit || rc.IsolationLevel != "read_committed" {
		t.Fatalf("unexpected receiver kafka fields: %+v", rc)
	}
}
