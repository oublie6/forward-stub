package packet

import "testing"

func TestPacketCloneCopiesKafkaRecordKeyAndKeepsPayloadIndependent(t *testing.T) {
	released := 0
	p := &Packet{
		Envelope: Envelope{
			Kind:    PayloadKindStream,
			Payload: []byte("payload"),
			Meta: Meta{
				Proto:          ProtoKafka,
				ReceiverName:   "rx",
				Remote:         "topic-in",
				Local:          "group-a",
				MatchKey:       "selector-key",
				KafkaRecordKey: "record-key",
				RouteSender:    "sender-a",
			},
		},
		ReleaseFn: func() { released++ },
	}

	cp := p.Clone()
	defer cp.Release()

	if cp == p {
		t.Fatal("clone returned original packet")
	}
	if cp.Kind != p.Kind {
		t.Fatalf("kind got=%v want=%v", cp.Kind, p.Kind)
	}
	if cp.Meta.KafkaRecordKey != p.Meta.KafkaRecordKey {
		t.Fatalf("kafka record key got=%q want=%q", cp.Meta.KafkaRecordKey, p.Meta.KafkaRecordKey)
	}
	if cp.Meta.MatchKey != p.Meta.MatchKey {
		t.Fatalf("match key got=%q want=%q", cp.Meta.MatchKey, p.Meta.MatchKey)
	}
	if cp.Meta.RouteSender != p.Meta.RouteSender {
		t.Fatalf("route sender got=%q want=%q", cp.Meta.RouteSender, p.Meta.RouteSender)
	}
	if string(cp.Payload) != string(p.Payload) {
		t.Fatalf("payload got=%q want=%q", cp.Payload, p.Payload)
	}
	if len(cp.Payload) == 0 || &cp.Payload[0] == &p.Payload[0] {
		t.Fatal("clone payload aliases original payload")
	}

	p.Payload[0] = 'P'
	p.Meta.KafkaRecordKey = "record-key-updated"
	if string(cp.Payload) != "payload" {
		t.Fatalf("clone payload changed after original mutation: %q", cp.Payload)
	}
	if cp.Meta.KafkaRecordKey != "record-key" {
		t.Fatalf("clone kafka record key changed after original mutation: %q", cp.Meta.KafkaRecordKey)
	}
	if released != 0 {
		t.Fatalf("clone should not call original release fn, released=%d", released)
	}
}

func TestPacketClonePreservesExistingMetaBehavior(t *testing.T) {
	p := &Packet{
		Envelope: Envelope{
			Kind:    PayloadKindFileChunk,
			Payload: []byte{1, 2, 3},
			Meta: Meta{
				Proto:          ProtoSFTP,
				ReceiverName:   "sftp-in",
				Remote:         "/remote/a.bin",
				Local:          "127.0.0.1:22",
				MatchKey:       "sftp|remote=/remote",
				TransferID:     "tx-1",
				FileName:       "a.bin",
				FilePath:       "/remote/a.bin",
				TargetFileName: "b.bin",
				TargetFilePath: "/out/b.bin",
				Offset:         7,
				TotalSize:      10,
				Checksum:       "sha256",
				EOF:            true,
				RouteSender:    "file-sender",
			},
		},
	}

	cp := p.Clone()
	defer cp.Release()

	if cp.Meta != p.Meta {
		t.Fatalf("meta changed during clone: got=%+v want=%+v", cp.Meta, p.Meta)
	}
	if string(cp.Payload) != string(p.Payload) {
		t.Fatalf("payload got=%v want=%v", cp.Payload, p.Payload)
	}
	if len(cp.Payload) == 0 || &cp.Payload[0] == &p.Payload[0] {
		t.Fatal("clone payload aliases original payload")
	}
}
