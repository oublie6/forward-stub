package runtime

import (
	"context"
	"reflect"
	"testing"

	"forward-stub/src/config"
)

func TestBuildReceiverUsesNormalizedConfigWithoutDefaultFallback(t *testing.T) {
	raw, err := buildReceiver("raw", config.ReceiverConfig{Type: "udp_gnet", Listen: ":0"}, "error")
	if err != nil {
		t.Fatalf("build raw receiver: %v", err)
	}
	rawValue := reflect.ValueOf(raw).Elem()
	if rawValue.FieldByName("multicore").Bool() {
		t.Fatalf("builder should not default raw multicore=true")
	}
	if got := int(rawValue.FieldByName("numEventLoop").Int()); got != 0 {
		t.Fatalf("builder should not default raw num_event_loop, got %d", got)
	}

	normalized := config.Config{
		Receivers: map[string]config.ReceiverConfig{"r1": {Type: "udp_gnet", Listen: ":0"}},
	}
	normalized.ApplyDefaults(config.BusinessDefaultsConfig{})
	built, err := buildReceiver("normalized", normalized.Receivers["r1"], "error")
	if err != nil {
		t.Fatalf("build normalized receiver: %v", err)
	}
	builtValue := reflect.ValueOf(built).Elem()
	if !builtValue.FieldByName("multicore").Bool() {
		t.Fatalf("normalized receiver should carry multicore default")
	}
	if got := int(builtValue.FieldByName("numEventLoop").Int()); got <= 0 {
		t.Fatalf("normalized receiver should carry num_event_loop default, got %d", got)
	}
}

func TestBuildSenderUsesNormalizedConfigWithoutDefaultFallback(t *testing.T) {
	raw, err := buildSender("raw", config.SenderConfig{
		Type:      "udp_unicast",
		LocalPort: 19000,
		Remote:    "127.0.0.1:19001",
	}, "error")
	if err != nil {
		t.Fatalf("build raw sender: %v", err)
	}
	defer func() { _ = raw.Close(context.Background()) }()
	rawValue := reflect.ValueOf(raw).Elem()
	if got := int(rawValue.FieldByName("concurrency").Int()); got != 1 {
		t.Fatalf("builder should pass raw concurrency to sender constructor without config default, got %d", got)
	}

	normalized := config.Config{
		Senders: map[string]config.SenderConfig{"s1": {
			Type:      "udp_unicast",
			LocalPort: 19002,
			Remote:    "127.0.0.1:19003",
		}},
	}
	normalized.ApplyDefaults(config.BusinessDefaultsConfig{})
	built, err := buildSender("normalized", normalized.Senders["s1"], "error")
	if err != nil {
		t.Fatalf("build normalized sender: %v", err)
	}
	defer func() { _ = built.Close(context.Background()) }()
	builtValue := reflect.ValueOf(built).Elem()
	if got := int(builtValue.FieldByName("concurrency").Int()); got != config.DefaultSenderConcurrency {
		t.Fatalf("normalized sender should carry config concurrency default, got %d", got)
	}
}
