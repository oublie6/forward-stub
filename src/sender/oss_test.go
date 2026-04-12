package sender

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"forward-stub/src/config"
	"forward-stub/src/packet"

	"github.com/minio/minio-go/v7"
)

type fakeOSSAPI struct {
	mu          sync.Mutex
	partSizes   []int64
	partIDs     []int
	completed   bool
	completeKey string

	putStarted chan struct{}
	releasePut chan struct{}
	putOnce    sync.Once
}

func (f *fakeOSSAPI) NewMultipartUpload(context.Context, string, string, minio.PutObjectOptions) (string, error) {
	return "upload-1", nil
}

func (f *fakeOSSAPI) PutObjectPart(_ context.Context, _, _, _ string, partID int, data io.Reader, size int64, _ minio.PutObjectPartOptions) (minio.ObjectPart, error) {
	if f.putStarted != nil {
		f.putOnce.Do(func() { close(f.putStarted) })
	}
	if f.releasePut != nil {
		<-f.releasePut
	}
	_, _ = io.Copy(io.Discard, data)
	f.mu.Lock()
	f.partSizes = append(f.partSizes, size)
	f.partIDs = append(f.partIDs, partID)
	f.mu.Unlock()
	return minio.ObjectPart{PartNumber: partID, ETag: "etag"}, nil
}

func (f *fakeOSSAPI) CompleteMultipartUpload(_ context.Context, _, object, _ string, _ []minio.CompletePart, _ minio.PutObjectOptions) (minio.UploadInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completed = true
	f.completeKey = object
	return minio.UploadInfo{}, nil
}

type fakeNotifier struct {
	mu     sync.Mutex
	events []FileReadyEvent
	err    error
}

func (n *fakeNotifier) NotifyFileReady(_ context.Context, event FileReadyEvent) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.events = append(n.events, event)
	if n.err != nil {
		return n.err
	}
	return nil
}

func (n *fakeNotifier) Close(context.Context) error { return nil }

func (f *fakeOSSAPI) snapshot() (bool, string, []int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.completed, f.completeKey, append([]int64(nil), f.partSizes...)
}

func (n *fakeNotifier) eventCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.events)
}

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
	completed, completeKey, partSizes := api.snapshot()
	if !completed {
		t.Fatalf("multipart was not completed")
	}
	if !reflect.DeepEqual(partSizes, []int64{4, 2}) {
		t.Fatalf("part sizes got=%v", partSizes)
	}
	if completeKey != "out/in/a.txt" {
		t.Fatalf("complete key got=%q", completeKey)
	}
	if len(n.events) != 1 || n.events[0].FetchKey != "out/in/a.txt" || n.events[0].FilePath != "out/in/a.txt" {
		t.Fatalf("notify event mismatch: %+v", n.events)
	}
}

func TestOSSSenderClearsStateWhenNotifyFailsAfterComplete(t *testing.T) {
	api := &fakeOSSAPI{}
	n := &fakeNotifier{err: errors.New("notify down")}
	s := newOSSSenderWithAPI("oss", config.SenderConfig{Endpoint: "endpoint", Bucket: "bucket", KeyPrefix: "out"}, api, []FileReadyNotifier{n}, 4)
	p := packetWithChecksum([]byte("abcd"), packet.Meta{
		TransferID: "tx-notify-fail",
		FilePath:   "in/a.txt",
		FileName:   "a.txt",
		Offset:     0,
		TotalSize:  4,
		EOF:        true,
	})
	err := s.Send(context.Background(), p)
	if err == nil {
		t.Fatalf("expected notify failure error")
	}
	completed, _, _ := api.snapshot()
	if !completed {
		t.Fatalf("multipart should have completed before notify failure")
	}
	if _, ok := s.states["tx-notify-fail"]; ok {
		t.Fatalf("transfer state should be cleared after complete even when notify fails")
	}
}

func TestSFTPSenderClearsStateWhenNotifyFailsAfterCommit(t *testing.T) {
	n := &fakeNotifier{err: errors.New("notify down")}
	s := &SFTPSender{
		name:          "sftp",
		addr:          "127.0.0.1:22",
		states:        []map[string]*sftpTransferState{{"tx-sftp": {finalPath: "/out/a.txt"}}},
		transferShard: map[string]int{"tx-sftp": 0},
		notifiers:     []FileReadyNotifier{n},
	}
	p := &packet.Packet{Envelope: packet.Envelope{Meta: packet.Meta{TransferID: "tx-sftp", FilePath: "in/a.txt", TotalSize: 1}}}
	err := s.afterSFTPCommitLocked(context.Background(), 0, "tx-sftp", p, "/out/a.txt")
	if err == nil {
		t.Fatalf("expected notify failure error")
	}
	if _, ok := s.states[0]["tx-sftp"]; ok {
		t.Fatalf("sftp transfer state should be cleared after commit even when notify fails")
	}
	if _, ok := s.transferShard["tx-sftp"]; ok {
		t.Fatalf("sftp transfer shard should be cleared after commit even when notify fails")
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

func TestOSSSenderIgnoresDuplicateChunksAndWaitsForGap(t *testing.T) {
	api := &fakeOSSAPI{}
	s := newOSSSenderWithAPI("oss", config.SenderConfig{Endpoint: "endpoint", Bucket: "bucket"}, api, nil, 4)

	// 先到达 offset=4 的乱序 chunk 时只能暂存，不能提前上传或 complete。
	if err := s.Send(context.Background(), packetWithChecksum([]byte("ef"), packet.Meta{TransferID: "tx-gap", FilePath: "a.txt", Offset: 4, TotalSize: 6, EOF: true})); err != nil {
		t.Fatalf("send out-of-order tail: %v", err)
	}
	completed, _, partSizes := api.snapshot()
	if completed || len(partSizes) != 0 {
		t.Fatalf("tail chunk should wait for missing prefix, completed=%v parts=%v", completed, partSizes)
	}

	if err := s.Send(context.Background(), packetWithChecksum([]byte("ab"), packet.Meta{TransferID: "tx-gap", FilePath: "a.txt", Offset: 0, TotalSize: 6})); err != nil {
		t.Fatalf("send head: %v", err)
	}
	if err := s.Send(context.Background(), packetWithChecksum([]byte("ab"), packet.Meta{TransferID: "tx-gap", FilePath: "a.txt", Offset: 0, TotalSize: 6})); err != nil {
		t.Fatalf("send duplicate head: %v", err)
	}
	if err := s.Send(context.Background(), packetWithChecksum([]byte("cd"), packet.Meta{TransferID: "tx-gap", FilePath: "a.txt", Offset: 2, TotalSize: 6})); err != nil {
		t.Fatalf("send middle: %v", err)
	}

	completed, _, partSizes = api.snapshot()
	if !completed {
		t.Fatalf("multipart should complete once gap is filled")
	}
	if !reflect.DeepEqual(partSizes, []int64{4, 2}) {
		t.Fatalf("duplicate chunk should not create extra uploaded part, parts=%v", partSizes)
	}
}

func TestOSSSenderDoesNotBlockOtherTransfersDuringPartUpload(t *testing.T) {
	api := &fakeOSSAPI{putStarted: make(chan struct{}), releasePut: make(chan struct{})}
	s := newOSSSenderWithAPI("oss", config.SenderConfig{Endpoint: "endpoint", Bucket: "bucket"}, api, nil, 4)

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Send(context.Background(), packetWithChecksum([]byte("abcd"), packet.Meta{
			TransferID: "tx-blocked",
			FilePath:   "blocked.bin",
			Offset:     0,
			TotalSize:  8,
		}))
	}()

	select {
	case <-api.putStarted:
	case <-time.After(time.Second):
		t.Fatalf("first transfer did not enter blocked multipart upload")
	}

	done := make(chan error, 1)
	go func() {
		done <- s.Send(context.Background(), packetWithChecksum([]byte("x"), packet.Meta{
			TransferID: "tx-independent",
			FilePath:   "independent.bin",
			Offset:     0,
			TotalSize:  2,
		}))
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("independent transfer send failed: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("independent transfer was blocked by another transfer's multipart upload")
	}

	close(api.releasePut)
	if err := <-errCh; err != nil {
		t.Fatalf("blocked transfer send failed after release: %v", err)
	}
}

func BenchmarkOSSSenderMultipartComplete(b *testing.B) {
	api := &fakeOSSAPI{}
	s := newOSSSenderWithAPI("oss", config.SenderConfig{Endpoint: "endpoint", Bucket: "bucket"}, api, nil, 4)
	payload := []byte("abcd")
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	for i := 0; i < b.N; i++ {
		p := packetWithChecksum(payload, packet.Meta{
			TransferID: "bench-" + strconv.Itoa(i),
			FilePath:   "bench.bin",
			Offset:     0,
			TotalSize:  int64(len(payload)),
			EOF:        true,
		})
		if err := s.Send(context.Background(), p); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOSSSenderParallelTransfers(b *testing.B) {
	api := &fakeOSSAPI{}
	s := newOSSSenderWithAPI("oss", config.SenderConfig{Endpoint: "endpoint", Bucket: "bucket"}, api, nil, 4)
	payload := []byte("abcd")
	var seq atomic.Uint64
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			id := seq.Add(1)
			p := packetWithChecksum(payload, packet.Meta{
				TransferID: "bench-par-" + strconv.FormatUint(id, 10),
				FilePath:   "bench.bin",
				Offset:     0,
				TotalSize:  int64(len(payload)),
				EOF:        true,
			})
			if err := s.Send(context.Background(), p); err != nil {
				b.Fatal(err)
			}
		}
	})
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
