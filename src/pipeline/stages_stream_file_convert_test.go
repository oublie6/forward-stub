package pipeline

import (
	"strings"
	"testing"

	"forward-stub/src/packet"
)

func makeStreamPacket(payload string, receiverName string, matchKey string) *packet.Packet {
	return &packet.Packet{
		Envelope: packet.Envelope{
			Kind:    packet.PayloadKindStream,
			Payload: []byte(payload),
			Meta: packet.Meta{
				Proto:        packet.ProtoUDP,
				ReceiverName: receiverName,
				MatchKey:     matchKey,
			},
		},
	}
}

func TestSplitFileChunkToPackets(t *testing.T) {
	st := SplitFileChunkToPackets(4, false)
	in := &packet.Packet{
		Envelope: packet.Envelope{
			Kind:    packet.PayloadKindFileChunk,
			Payload: []byte("abcdefghij"),
			Meta: packet.Meta{
				FilePath:   "/in/a.bin",
				FileName:   "a.bin",
				TransferID: "tid",
				Offset:     100,
				TotalSize:  10,
				EOF:        true,
			},
		},
	}
	out := st([]*packet.Packet{in})
	if len(out) != 3 {
		t.Fatalf("expected 3 packets, got %d", len(out))
	}
	wantPayload := []string{"abcd", "efgh", "ij"}
	wantOffset := []int64{100, 104, 108}
	for i := range out {
		if got := string(out[i].Payload); got != wantPayload[i] {
			t.Fatalf("payload[%d] mismatch: %q", i, got)
		}
		if out[i].Meta.Offset != wantOffset[i] {
			t.Fatalf("offset[%d] mismatch: %d", i, out[i].Meta.Offset)
		}
		if out[i].Meta.EOF != (i == 2) {
			t.Fatalf("eof[%d] mismatch: %v", i, out[i].Meta.EOF)
		}
		if out[i].Kind != packet.PayloadKindStream {
			t.Fatalf("kind[%d] should be stream", i)
		}
		if out[i].Meta.TransferID != "" || out[i].Meta.FilePath != "" || out[i].Meta.FileName != "" {
			t.Fatalf("file meta should be cleared when preserve=false")
		}
		out[i].Release()
	}
}

func TestSplitFileChunkToPacketsPreserveMeta(t *testing.T) {
	st := SplitFileChunkToPackets(3, true)
	in := &packet.Packet{
		Envelope: packet.Envelope{
			Kind:    packet.PayloadKindFileChunk,
			Payload: []byte("abcdef"),
			Meta: packet.Meta{
				FilePath:   "/in/a.bin",
				FileName:   "a.bin",
				TransferID: "tid",
				Offset:     1,
				TotalSize:  6,
				EOF:        true,
			},
		},
	}
	out := st([]*packet.Packet{in})
	if len(out) != 2 {
		t.Fatalf("expected 2 packets, got %d", len(out))
	}
	if out[0].Meta.TransferID != "tid" || out[1].Meta.TransferID != "tid" {
		t.Fatalf("expected transfer id preserved")
	}
	if out[0].Meta.EOF || !out[1].Meta.EOF {
		t.Fatalf("expected EOF only on last packet")
	}
	out[0].Release()
	out[1].Release()
}

func TestStreamPacketsToFileSegmentsRolling(t *testing.T) {
	st := StreamPacketsToFileSegments(10, 4, "/out", "seg", "20060102")
	in1 := makeStreamPacket("abcdefghijkl", "udp-rx", "udp|src_addr=10.0.0.1:9000")
	out1 := st([]*packet.Packet{in1})
	if len(out1) != 3 {
		t.Fatalf("expected 3 chunks for first full segment, got %d", len(out1))
	}
	if string(out1[0].Payload) != "abcd" || string(out1[1].Payload) != "efgh" || string(out1[2].Payload) != "ij" {
		t.Fatalf("unexpected chunk payloads")
	}
	if out1[0].Meta.Offset != 0 || out1[1].Meta.Offset != 4 || out1[2].Meta.Offset != 8 {
		t.Fatalf("offsets not incremental")
	}
	if out1[0].Meta.EOF || out1[1].Meta.EOF || !out1[2].Meta.EOF {
		t.Fatalf("EOF should only be on last chunk in segment")
	}
	firstPath := out1[0].Meta.FilePath
	firstID := out1[0].Meta.TransferID
	if !strings.Contains(firstPath, "/out/seg_") || !strings.HasSuffix(firstPath, "_000001.bin") {
		t.Fatalf("unexpected first path: %s", firstPath)
	}
	for _, p := range out1 {
		if p.Meta.TransferID != firstID {
			t.Fatalf("first segment transfer id should be stable")
		}
		if p.Meta.ReceiverName != "udp-rx" || p.Meta.MatchKey != "udp|src_addr=10.0.0.1:9000" {
			t.Fatalf("stream meta should keep receiver+match context")
		}
		p.Release()
	}

	in2 := makeStreamPacket("mnopqrst", "udp-rx", "udp|src_addr=10.0.0.1:9000")
	out2 := st([]*packet.Packet{in2})
	if len(out2) != 3 {
		t.Fatalf("expected 3 chunks for second segment, got %d", len(out2))
	}
	secondPath := out2[0].Meta.FilePath
	if secondPath == firstPath || !strings.HasSuffix(secondPath, "_000002.bin") {
		t.Fatalf("expected rotated segment path, got %s", secondPath)
	}
	if out2[2].Meta.EOF != true {
		t.Fatalf("second segment should end with EOF")
	}
	for _, p := range out2 {
		p.Release()
	}
}

func TestStreamPacketsToFileSegmentsSeparatesBuffersByReceiverName(t *testing.T) {
	st := StreamPacketsToFileSegments(4, 2, "/out", "seg", "20060102")

	out := st([]*packet.Packet{
		makeStreamPacket("ab", "receiver_a", "fixed|same"),
		makeStreamPacket("cd", "receiver_b", "fixed|same"),
	})
	if len(out) != 0 {
		t.Fatalf("different receiver with same match_key should not flush together, got %d packets", len(out))
	}

	out = st([]*packet.Packet{
		makeStreamPacket("ef", "receiver_a", "fixed|same"),
		makeStreamPacket("gh", "receiver_b", "fixed|same"),
	})
	if len(out) != 4 {
		t.Fatalf("expected two independent buffers to flush into 4 chunks, got %d", len(out))
	}
	if string(out[0].Payload) != "ab" || string(out[1].Payload) != "ef" {
		t.Fatalf("receiver_a buffer mixed unexpectedly: %q %q", out[0].Payload, out[1].Payload)
	}
	if string(out[2].Payload) != "cd" || string(out[3].Payload) != "gh" {
		t.Fatalf("receiver_b buffer mixed unexpectedly: %q %q", out[2].Payload, out[3].Payload)
	}
	if out[0].Meta.TransferID == out[2].Meta.TransferID {
		t.Fatalf("different receiver should not share transfer_id")
	}
	for _, p := range out {
		p.Release()
	}
}

func TestStreamPacketsToFileSegmentsSeparatesBuffersByMatchKey(t *testing.T) {
	st := StreamPacketsToFileSegments(4, 2, "/out", "seg", "20060102")

	out := st([]*packet.Packet{
		makeStreamPacket("ab", "receiver_a", "match_key_1"),
		makeStreamPacket("cd", "receiver_a", "match_key_2"),
	})
	if len(out) != 0 {
		t.Fatalf("same receiver with different match_key should not flush together, got %d packets", len(out))
	}

	out = st([]*packet.Packet{
		makeStreamPacket("ef", "receiver_a", "match_key_1"),
		makeStreamPacket("gh", "receiver_a", "match_key_2"),
	})
	if len(out) != 4 {
		t.Fatalf("expected two independent buffers to flush into 4 chunks, got %d", len(out))
	}
	if string(out[0].Payload) != "ab" || string(out[1].Payload) != "ef" {
		t.Fatalf("match_key_1 buffer mixed unexpectedly: %q %q", out[0].Payload, out[1].Payload)
	}
	if string(out[2].Payload) != "cd" || string(out[3].Payload) != "gh" {
		t.Fatalf("match_key_2 buffer mixed unexpectedly: %q %q", out[2].Payload, out[3].Payload)
	}
	if out[0].Meta.TransferID == out[2].Meta.TransferID {
		t.Fatalf("different match_key should not share transfer_id")
	}
	for _, p := range out {
		p.Release()
	}
}

func TestStreamPacketsToFileSegmentsKeepsSameReceiverAndMatchKeyInOneBuffer(t *testing.T) {
	st := StreamPacketsToFileSegments(6, 3, "/out", "seg", "20060102")

	out := st([]*packet.Packet{
		makeStreamPacket("abc", "receiver_a", "match_key_same"),
	})
	if len(out) != 0 {
		t.Fatalf("same receiver+match first partial write should stay buffered, got %d packets", len(out))
	}

	out = st([]*packet.Packet{
		makeStreamPacket("def", "receiver_a", "match_key_same"),
	})
	if len(out) != 2 {
		t.Fatalf("same receiver+match should flush one segment into 2 chunks, got %d", len(out))
	}
	if string(out[0].Payload) != "abc" || string(out[1].Payload) != "def" {
		t.Fatalf("same receiver+match should stay in one buffer, got %q %q", out[0].Payload, out[1].Payload)
	}
	if out[0].Meta.TransferID != out[1].Meta.TransferID {
		t.Fatalf("same receiver+match should share one transfer_id")
	}
	if out[0].Meta.EOF || !out[1].Meta.EOF {
		t.Fatalf("EOF should only appear on last chunk of same buffer")
	}
	for _, p := range out {
		p.Release()
	}
}

func TestStreamPacketsToFileSegmentsRejectsEmptyReceiverName(t *testing.T) {
	st := StreamPacketsToFileSegments(4, 2, "/out", "seg", "20060102")
	out := st([]*packet.Packet{makeStreamPacket("abcd", "", "match_key_same")})
	if len(out) != 0 {
		t.Fatalf("empty receiver_name should be rejected, got %d packets", len(out))
	}
}

func TestStreamPacketsToFileSegmentsRejectsEmptyMatchKey(t *testing.T) {
	st := StreamPacketsToFileSegments(4, 2, "/out", "seg", "20060102")
	out := st([]*packet.Packet{makeStreamPacket("abcd", "receiver_a", "")})
	if len(out) != 0 {
		t.Fatalf("empty match_key should be rejected, got %d packets", len(out))
	}
}
