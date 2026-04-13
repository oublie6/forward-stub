package config

import (
	"encoding/json"
	"testing"
)

func TestNotifyOnSuccessUnmarshalObjectAndArray(t *testing.T) {
	// notify_on_success 最初只支持单对象；这里锁定对象/数组两种 JSON 写法，避免后续改动破坏 OSS sender 兼容配置。
	var one struct {
		Notify NotifyOnSuccessConfigs `json:"notify_on_success"`
	}
	if err := json.Unmarshal([]byte(`{"notify_on_success":{"type":"kafka","remote":"127.0.0.1:9092","topic":"ready"}}`), &one); err != nil {
		t.Fatalf("unmarshal single notify object: %v", err)
	}
	if len(one.Notify) != 1 || one.Notify[0].Type != "kafka" || one.Notify[0].Topic != "ready" {
		t.Fatalf("unexpected single notify decode: %+v", one.Notify)
	}

	var many struct {
		Notify NotifyOnSuccessConfigs `json:"notify_on_success"`
	}
	if err := json.Unmarshal([]byte(`{"notify_on_success":[{"type":"kafka","remote":"127.0.0.1:9092","topic":"ready"},{"type":"dds_skydds","dcps_config_file":"dds.ini","topic_name":"Ready","message_model":"octet"}]}`), &many); err != nil {
		t.Fatalf("unmarshal notify array: %v", err)
	}
	if len(many.Notify) != 2 || many.Notify[1].Type != "dds_skydds" {
		t.Fatalf("unexpected notify array decode: %+v", many.Notify)
	}
}

func TestNotifyOnSuccessUnmarshalNull(t *testing.T) {
	var got NotifyOnSuccessConfigs
	if err := json.Unmarshal([]byte(`null`), &got); err != nil {
		t.Fatalf("unmarshal null notify: %v", err)
	}
	if got != nil {
		t.Fatalf("null notify should decode to nil, got %#v", got)
	}
}
