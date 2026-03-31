package sender

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"forward-stub/src/config"
	"forward-stub/src/kafkautil"
	"forward-stub/src/packet"

	"github.com/twmb/franz-go/pkg/kgo"
)

func TestNewKafkaSenderAppliesBufferedOptions(t *testing.T) {
	s, err := NewKafkaSender("k1", config.SenderConfig{
		Type:               "kafka",
		Remote:             "127.0.0.1:9092",
		Topic:              "out",
		Concurrency:        1,
		MaxBufferedBytes:   2048,
		MaxBufferedRecords: 128,
	})
	if err != nil {
		t.Fatalf("new kafka sender: %v", err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	cli := s.clients[0].Load()
	if cli == nil {
		t.Fatalf("kafka client is nil")
	}
	if got := cli.OptValue(kgo.MaxBufferedBytes).(int64); got != 2048 {
		t.Fatalf("unexpected max buffered bytes: got %d want %d", got, 2048)
	}
	if got := cli.OptValue(kgo.MaxBufferedRecords).(int64); got != 128 {
		t.Fatalf("unexpected max buffered records: got %d want %d", got, 128)
	}
}

func TestNewKafkaSenderKeepsFranzDefaultsForBufferedOptionsWhenUnset(t *testing.T) {
	s, err := NewKafkaSender("k1", config.SenderConfig{
		Type:        "kafka",
		Remote:      "127.0.0.1:9092",
		Topic:       "out",
		Concurrency: 1,
	})
	if err != nil {
		t.Fatalf("new kafka sender: %v", err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	cli := s.clients[0].Load()
	if cli == nil {
		t.Fatalf("kafka client is nil")
	}
	if got := cli.OptValue(kgo.MaxBufferedBytes).(int64); got != 0 {
		t.Fatalf("unexpected default max buffered bytes: got %d want %d", got, 0)
	}
	if got := cli.OptValue(kgo.MaxBufferedRecords).(int64); got != 10000 {
		t.Fatalf("unexpected default max buffered records: got %d want %d", got, 10000)
	}
}

func TestNewKafkaSenderAppliesKafkaTimingAndPartitionerOptions(t *testing.T) {
	s, err := NewKafkaSender("k1", config.SenderConfig{
		Type:            "kafka",
		Remote:          "127.0.0.1:9092",
		Topic:           "out",
		Concurrency:     1,
		DialTimeout:     "11s",
		RequestTimeout:  "31s",
		RetryTimeout:    "2m",
		RetryBackoff:    "300ms",
		ConnIdleTimeout: "45s",
		MetadataMaxAge:  "7m",
		Partitioner:     "round_robin",
	})
	if err != nil {
		t.Fatalf("new kafka sender: %v", err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	cli := s.clients[0].Load()
	if cli == nil {
		t.Fatalf("kafka client is nil")
	}
	if got := cli.OptValue(kgo.DialTimeout).(time.Duration); got != 11*time.Second {
		t.Fatalf("unexpected dial timeout: %s", got)
	}
	if got := cli.OptValue(kgo.ProduceRequestTimeout).(time.Duration); got != 31*time.Second {
		t.Fatalf("unexpected request timeout: %s", got)
	}
	if got := cli.OptValue(kgo.RetryTimeout).(time.Duration); got != 2*time.Minute {
		t.Fatalf("unexpected retry timeout: %s", got)
	}
	backoffFn := cli.OptValue(kgo.RetryBackoffFn).(func(int) time.Duration)
	if got := backoffFn(3); got != 300*time.Millisecond {
		t.Fatalf("unexpected retry backoff: %s", got)
	}
	if got := cli.OptValue(kgo.ConnIdleTimeout).(time.Duration); got != 45*time.Second {
		t.Fatalf("unexpected conn idle timeout: %s", got)
	}
	if got := cli.OptValue(kgo.MetadataMaxAge).(time.Duration); got != 7*time.Minute {
		t.Fatalf("unexpected metadata max age: %s", got)
	}
	if got := reflect.TypeOf(cli.OptValue(kgo.RecordPartitioner)).String(); !strings.Contains(got, "roundRobinPartitioner") {
		t.Fatalf("unexpected partitioner type: %s", got)
	}
}

func TestKafkaRecordKeyUsesConfiguredSource(t *testing.T) {
	pkt := &packet.Packet{
		Envelope: packet.Envelope{
			Payload: []byte("payload"),
			Meta: packet.Meta{
				MatchKey:    "kafka|topic=in|partition=0",
				Remote:      "remote",
				Local:       "local",
				FileName:    "a.txt",
				FilePath:    "/tmp/a.txt",
				TransferID:  "tx-1",
				RouteSender: "tx_kafka",
			},
		},
	}
	if got := string(kafkautil.RecordKey([]byte("fixed"), "", pkt)); got != "fixed" {
		t.Fatalf("unexpected fixed key: %q", got)
	}
	if got := string(kafkautil.RecordKey(nil, "match_key", pkt)); got != pkt.Meta.MatchKey {
		t.Fatalf("unexpected match_key source: %q", got)
	}
	if got := string(kafkautil.RecordKey(nil, "payload", pkt)); got != "payload" {
		t.Fatalf("unexpected payload source: %q", got)
	}
}

func TestKafkaCompressionCodecAppliesLevel(t *testing.T) {
	codec, enabled, err := kafkautil.CompressionCodec("zstd", 7)
	if err != nil {
		t.Fatalf("compression codec: %v", err)
	}
	if !enabled {
		t.Fatal("expected compression enabled")
	}
	if compressor, err := kgo.DefaultCompressor(codec); err != nil || compressor == nil {
		t.Fatalf("expected valid compressor, err=%v", err)
	}
}
