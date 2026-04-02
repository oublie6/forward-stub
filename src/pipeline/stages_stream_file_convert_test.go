package pipeline

import (
	"strings"
	"testing"

	"forward-stub/src/packet"
)

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
	in1 := &packet.Packet{Envelope: packet.Envelope{Kind: packet.PayloadKindStream, Payload: []byte("abcdefghijkl"), Meta: packet.Meta{Proto: packet.ProtoUDP}}}
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
		p.Release()
	}

	in2 := &packet.Packet{Envelope: packet.Envelope{Kind: packet.PayloadKindStream, Payload: []byte("mnopqrst"), Meta: packet.Meta{Proto: packet.ProtoUDP}}}
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
