package config

import (
	"encoding/json"
	"testing"
)

// TestSenderConfigKafkaBufferFieldsUnmarshal 验证 config 包中 SenderConfigKafkaBufferFieldsUnmarshal 的行为。
func TestSenderConfigKafkaBufferFieldsUnmarshal(t *testing.T) {
	var sc SenderConfig
	if err := json.Unmarshal([]byte(`{"type":"kafka","remote":"127.0.0.1:9092","topic":"out","max_buffered_bytes":1048576,"max_buffered_records":5000}`), &sc); err != nil {
		t.Fatalf("unmarshal sender config: %v", err)
	}
	if sc.MaxBufferedBytes != 1048576 {
		t.Fatalf("unexpected max_buffered_bytes: %d", sc.MaxBufferedBytes)
	}
	if sc.MaxBufferedRecords != 5000 {
		t.Fatalf("unexpected max_buffered_records: %d", sc.MaxBufferedRecords)
	}
}
