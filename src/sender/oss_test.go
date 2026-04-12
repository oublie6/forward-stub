package sender

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"reflect"
	"testing"

	"forward-stub/src/config"
	"forward-stub/src/packet"

	"github.com/minio/minio-go/v7"
)

type fakeOSSAPI struct {
	partSizes   []int64
	completed   bool
	completeKey string
}

func (f *fakeOSSAPI) NewMultipartUpload(context.Context, string, string, minio.PutObjectOptions) (string, error) {
	return "upload-1", nil
}

func (f *fakeOSSAPI) PutObjectPart(_ context.Context, _, _, _ string, partID int, data io.Reader, size int64, _ minio.PutObjectPartOptions) (minio.ObjectPart, error) {
	_, _ = io.Copy(io.Discard, data)
	f.partSizes = append(f.partSizes, size)
	return minio.ObjectPart{PartNumber: partID, ETag: "etag"}, nil
}

func (f *fakeOSSAPI) CompleteMultipartUpload(_ context.Context, _, object, _ string, _ []minio.CompletePart, _ minio.PutObjectOptions) (minio.UploadInfo, error) {
	f.completed = true
	f.completeKey = object
	return minio.UploadInfo{}, nil
}

type fakeNotifier struct {
	events []FileReadyEvent
}

func (n *fakeNotifier) NotifyFileReady(_ context.Context, event FileReadyEvent) error {
	n.events = append(n.events, event)
	return nil
}

func (n *fakeNotifier) Close(context.Context) error { return nil }

func TestOSSObjectKeyRules(t *testing.T) {
	p := &packet.Packet{Envelope: packet.Envelope{Meta: packet.Meta{TargetFilePath: "/target/a.csv", FilePath: "/src/b.csv", FileName: "b.csv"}}}
	if got, err := ossObjectKey("ignored", p); err != nil || got != "target/a.csv" {
		t.Fatalf("target path key got=%q err=%v", got, err)
	}
	p.Meta.TargetFilePath = ""
	if got, err := ossObjectKey("out", p); err != nil || got != "out/src/b.csv" {
		t.Fatalf("file path key got=%q err=%v", got, err)
	}
	p.Meta.FilePath = ""
	if got, err := ossObjectKey("out", p); err != nil || got != "out/b.csv" {
		t.Fatalf("file name key got=%q err=%v", got, err)
	}
	p.Meta.FileName = "../bad.csv"
	if _, err := ossObjectKey("", p); err == nil {
		t.Fatalf("unsafe key should fail")
	}
}

func TestOSSSenderRejectsNonFileChunk(t *testing.T) {
	s := newOSSSenderWithAPI("oss", config.SenderConfig{Endpoint: "e", Bucket: "b"}, &fakeOSSAPI{}, nil, 4)
	p := &packet.Packet{Envelope: packet.Envelope{Kind: packet.PayloadKindStream, Payload: []byte("x")}}
	if err := s.Send(context.Background(), p); err == nil {
		t.Fatalf("expected non file_chunk error")
	}
}

func TestOSSSenderAggregatesChunksAndCompletesAfterEOF(t *testing.T) {
	api := &fakeOSSAPI{}
	n := &fakeNotifier{}
	s := newOSSSenderWithAPI("oss", config.SenderConfig{Endpoint: "endpoint", Bucket: "bucket", KeyPrefix: "out"}, api, []FileReadyNotifier{n}, 4)
	chunks := []struct {
		off int64
		buf []byte
		eof bool
	}{
		{0, []byte("ab"), false},
		{2, []byte("cd"), false},
		{4, []byte("ef"), true},
	}
	for _, c := range chunks {
		p := packetWithChecksum(c.buf, packet.Meta{
			TransferID: "tx1",
			FilePath:   "in/a.txt",
			FileName:   "a.txt",
			Offset:     c.off,
			TotalSize:  6,
			EOF:        c.eof,
		})
		if err := s.Send(context.Background(), p); err != nil {
			t.Fatalf("send offset %d: %v", c.off, err)
		}
	}
	if !api.completed {
		t.Fatalf("multipart was not completed")
	}
	if !reflect.DeepEqual(api.partSizes, []int64{4, 2}) {
		t.Fatalf("part sizes got=%v", api.partSizes)
	}
	if api.completeKey != "out/in/a.txt" {
		t.Fatalf("complete key got=%q", api.completeKey)
	}
	if len(n.events) != 1 || n.events[0].FetchKey != "out/in/a.txt" || n.events[0].FilePath != "out/in/a.txt" {
		t.Fatalf("notify event mismatch: %+v", n.events)
	}
}

func TestOSSRangeMergeReadyToComplete(t *testing.T) {
	st := &ossTransferState{totalSize: 6, pendingSegments: map[int64][]byte{}}
	st.addRange(2, 4)
	st.addRange(0, 2)
	st.addRange(4, 6)
	st.eofSeen = true
	st.nextOffset = 6
	if !st.readyToComplete() {
		t.Fatalf("expected complete range to be ready")
	}
}

func packetWithChecksum(payload []byte, meta packet.Meta) *packet.Packet {
	h := sha256sum(payload)
	meta.Checksum = h
	return &packet.Packet{Envelope: packet.Envelope{Kind: packet.PayloadKindFileChunk, Payload: payload, Meta: meta}}
}

func sha256sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
