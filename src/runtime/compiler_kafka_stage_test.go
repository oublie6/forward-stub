package runtime

import (
	"testing"

	"forward-stub/src/config"
	"forward-stub/src/packet"
)

func TestCompilePipelineSetKafkaRecordKeyFromOffsetBytes(t *testing.T) {
	compiled, err := CompilePipelines(map[string][]config.StageConfig{
		"p": {{Type: "set_kafka_record_key_from_offset_bytes", Offset: 1, Length: 2, Encoding: "hex"}},
	})
	if err != nil {
		t.Fatalf("compile pipeline: %v", err)
	}

	p := &packet.Packet{Envelope: packet.Envelope{Payload: []byte{0x00, 0xab, 0xcd}}}
	out := compiled["p"].P.Process(p)
	if len(out) != 1 {
		t.Fatalf("pipeline should pass")
	}
	if p.Meta.KafkaRecordKey != "abcd" {
		t.Fatalf("kafka record key got=%q want abcd", p.Meta.KafkaRecordKey)
	}
}

func TestCompilePipelineRejectsKafkaRecordKeyEncoding(t *testing.T) {
	_, err := CompilePipelines(map[string][]config.StageConfig{
		"p": {{Type: "set_kafka_record_key_from_offset_bytes", Offset: 0, Length: 1, Encoding: "base64"}},
	})
	if err == nil {
		t.Fatal("expected compile error")
	}
}
