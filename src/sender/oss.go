package sender

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"forward-stub/src/config"
	"forward-stub/src/logx"
	"forward-stub/src/packet"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const defaultOSSPartSize = config.DefaultOSSPartSize

type ossMultipartAPI interface {
	NewMultipartUpload(ctx context.Context, bucket, object string, opts minio.PutObjectOptions) (string, error)
	PutObjectPart(ctx context.Context, bucket, object, uploadID string, partID int, data io.Reader, size int64, opts minio.PutObjectPartOptions) (minio.ObjectPart, error)
	CompleteMultipartUpload(ctx context.Context, bucket, object, uploadID string, parts []minio.CompletePart, opts minio.PutObjectOptions) (minio.UploadInfo, error)
}

type minioCoreAPI struct{ core *minio.Core }

func (m minioCoreAPI) NewMultipartUpload(ctx context.Context, bucket, object string, opts minio.PutObjectOptions) (string, error) {
	return m.core.NewMultipartUpload(ctx, bucket, object, opts)
}

func (m minioCoreAPI) PutObjectPart(ctx context.Context, bucket, object, uploadID string, partID int, data io.Reader, size int64, opts minio.PutObjectPartOptions) (minio.ObjectPart, error) {
	return m.core.PutObjectPart(ctx, bucket, object, uploadID, partID, data, size, opts)
}

func (m minioCoreAPI) CompleteMultipartUpload(ctx context.Context, bucket, object, uploadID string, parts []minio.CompletePart, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	return m.core.CompleteMultipartUpload(ctx, bucket, object, uploadID, parts, opts)
}

// OSSSender 是基于 S3-compatible multipart upload 的文件发送端。
//
// 运行模型：
//  1. 每个 transfer_id 创建一个 multipart upload session；
//  2. chunk 可乱序到达，连续区间进入 pendingPartBuffer，空洞后的区间进入有界 pendingSegments；
//  3. pendingPartBuffer 达到 part_size 后上传一个 part；
//  4. EOF 且覆盖区间完整后上传尾 part、complete 对象并触发通知。
type OSSSender struct {
	name         string
	endpoint     string
	bucket       string
	keyPrefix    string
	partSize     int64
	putOpts      minio.PutObjectOptions
	api          ossMultipartAPI
	notifiers    []FileReadyNotifier
	mu           sync.Mutex
	states       map[string]*ossTransferState
	pendingLimit int64
}

type ossTransferState struct {
	// objectKey/uploadID 在首个 chunk 到达时确定，后续同一 transfer_id 固定复用。
	objectKey string
	uploadID  string
	totalSize int64
	eofSeen   bool
	// coveredRanges 记录“已收到”的文件区间，用来判断是否存在空洞。
	// 它和 nextOffset 分开维护：前者判断完整性，后者驱动可连续上传的字节流。
	coveredRanges []fileRange
	coveredBytes  int64

	// nextOffset 是当前已连续接收并写入 pendingPartBuffer 的文件偏移。
	nextOffset        int64
	pendingPartBuffer []byte
	// pendingSegments 暂存 offset > nextOffset 的乱序 chunk；pendingLimit 限制其总内存。
	pendingSegments map[int64][]byte
	pendingBytes    int64
	uploadedParts   []ossUploadedPart
	nextPartNumber  int
}

type ossUploadedPart struct {
	partNumber int
	etag       string
	start      int64
	end        int64
}

// NewOSSSender 构造 OSSSender 并创建底层 minio Core。
// 调用方应先执行 Config.ApplyDefaults + Validate；若 part_size 仍未设置，这里会再次回退默认值作为保护。
func NewOSSSender(name string, sc config.SenderConfig) (*OSSSender, error) {
	if strings.TrimSpace(sc.Endpoint) == "" {
		return nil, fmt.Errorf("oss sender requires endpoint")
	}
	if strings.TrimSpace(sc.Bucket) == "" {
		return nil, fmt.Errorf("oss sender requires bucket")
	}
	if strings.TrimSpace(sc.AccessKey) == "" || strings.TrimSpace(sc.SecretKey) == "" {
		return nil, fmt.Errorf("oss sender requires access_key and secret_key")
	}
	partSize := sc.PartSize
	if partSize <= 0 {
		partSize = defaultOSSPartSize
	}
	opts := &minio.Options{
		Creds:  credentials.NewStaticV4(strings.TrimSpace(sc.AccessKey), strings.TrimSpace(sc.SecretKey), ""),
		Secure: sc.UseSSL,
		Region: strings.TrimSpace(sc.Region),
	}
	if sc.ForcePathStyle {
		opts.BucketLookup = minio.BucketLookupPath
	}
	core, err := minio.NewCore(strings.TrimSpace(sc.Endpoint), opts)
	if err != nil {
		return nil, err
	}
	notifiers, err := buildFileReadyNotifiers(sc.NotifyOnSuccess)
	if err != nil {
		return nil, err
	}
	return newOSSSenderWithAPI(name, sc, minioCoreAPI{core: core}, notifiers, partSize), nil
}

func newOSSSenderWithAPI(name string, sc config.SenderConfig, api ossMultipartAPI, notifiers []FileReadyNotifier, partSize int64) *OSSSender {
	if partSize <= 0 {
		partSize = defaultOSSPartSize
	}
	return &OSSSender{
		name:         name,
		endpoint:     strings.TrimSpace(sc.Endpoint),
		bucket:       strings.TrimSpace(sc.Bucket),
		keyPrefix:    strings.TrimSpace(sc.KeyPrefix),
		partSize:     partSize,
		putOpts:      minio.PutObjectOptions{StorageClass: strings.TrimSpace(sc.StorageClass), ContentType: strings.TrimSpace(sc.ContentType)},
		api:          api,
		notifiers:    notifiers,
		states:       make(map[string]*ossTransferState),
		pendingLimit: partSize * 2,
	}
}

func (s *OSSSender) Name() string { return s.name }

func (s *OSSSender) Key() string { return "oss|" + s.endpoint + "|" + s.bucket + "|" + s.keyPrefix }

// Send 接收一个 file_chunk 并推进 multipart 状态机。
// 重复 chunk 会被幂等忽略；乱序 chunk 只在有界缓存内等待，避免上游缺口导致 sender 内存无限增长。
func (s *OSSSender) Send(ctx context.Context, p *packet.Packet) error {
	if p.Kind != packet.PayloadKindFileChunk {
		return fmt.Errorf("oss sender only accepts file_chunk payload")
	}
	if want := strings.TrimSpace(p.Meta.Checksum); want != "" {
		h := sha256.Sum256(p.Payload)
		if got := hex.EncodeToString(h[:]); !strings.EqualFold(got, want) {
			return fmt.Errorf("oss sender checksum mismatch: got=%s want=%s transfer=%s", got, want, p.Meta.TransferID)
		}
	}
	transferID := strings.TrimSpace(p.Meta.TransferID)
	if transferID == "" {
		return fmt.Errorf("oss sender requires transfer_id")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	st, ok := s.states[transferID]
	if !ok {
		objectKey, err := ossObjectKey(s.keyPrefix, p)
		if err != nil {
			return err
		}
		uploadID, err := s.api.NewMultipartUpload(ctx, s.bucket, objectKey, s.putOpts)
		if err != nil {
			return err
		}
		st = &ossTransferState{
			objectKey:         objectKey,
			uploadID:          uploadID,
			totalSize:         p.Meta.TotalSize,
			pendingSegments:   make(map[int64][]byte),
			nextPartNumber:    1,
			pendingPartBuffer: make([]byte, 0, minInt64(s.partSize, 16<<20)),
		}
		s.states[transferID] = st
	}
	if p.Meta.TotalSize > 0 {
		st.totalSize = p.Meta.TotalSize
	}
	if p.Meta.EOF {
		st.eofSeen = true
	}
	st.addRange(p.Meta.Offset, p.Meta.Offset+int64(len(p.Payload)))
	if err := st.acceptChunk(p.Meta.Offset, p.Payload, s.pendingLimit); err != nil {
		return err
	}
	if err := st.flushReadyParts(ctx, s.api, s.bucket, s.partSize); err != nil {
		return err
	}
	if st.readyToComplete() {
		if len(st.pendingPartBuffer) > 0 {
			if err := st.uploadPart(ctx, s.api, s.bucket); err != nil {
				return err
			}
		}
		parts := st.completeParts()
		if _, err := s.api.CompleteMultipartUpload(ctx, s.bucket, st.objectKey, st.uploadID, parts, s.putOpts); err != nil {
			return err
		}
		objectKey := st.objectKey
		delete(s.states, transferID)
		if err := notifyFileReady(ctx, s.notifiers, ossFileReadyEvent(p, s.name, s.endpoint, s.bucket, objectKey)); err != nil {
			// TODO: 在这里接入通知 outbox / 持久化补发表。对象已经 complete 成功，不能再保留 multipart 未完成状态。
			logx.L().Errorw("OSS对象已complete成功，通知失败", "发送端", s.name, "bucket", s.bucket, "key", objectKey, "错误", err)
			return fmt.Errorf("oss sender object completed but notify failed: %w", err)
		}
	}
	return nil
}

func (s *OSSSender) Close(ctx context.Context) error {
	for _, n := range s.notifiers {
		_ = n.Close(ctx)
	}
	return nil
}

func (st *ossTransferState) acceptChunk(offset int64, payload []byte, pendingLimit int64) error {
	end := offset + int64(len(payload))
	if end <= st.nextOffset {
		return nil
	}
	if offset == st.nextOffset {
		st.pendingPartBuffer = append(st.pendingPartBuffer, payload...)
		st.nextOffset = end
		st.drainContiguousSegments()
		return nil
	}
	if _, exists := st.pendingSegments[offset]; exists {
		return nil
	}
	cp := append([]byte(nil), payload...)
	st.pendingSegments[offset] = cp
	st.pendingBytes += int64(len(cp))
	if st.pendingBytes > pendingLimit {
		return fmt.Errorf("oss sender pending out-of-order chunks exceed bounded limit: pending=%d limit=%d", st.pendingBytes, pendingLimit)
	}
	return nil
}

// drainContiguousSegments 把已补齐缺口后的乱序片段接到连续缓冲区。
// 只有 offset 恰好等于 nextOffset 的片段才会被消费，确保 multipart 上传顺序与文件偏移一致。
func (st *ossTransferState) drainContiguousSegments() {
	for {
		seg, ok := st.pendingSegments[st.nextOffset]
		if !ok {
			return
		}
		delete(st.pendingSegments, st.nextOffset)
		st.pendingBytes -= int64(len(seg))
		st.pendingPartBuffer = append(st.pendingPartBuffer, seg...)
		st.nextOffset += int64(len(seg))
	}
}

// flushReadyParts 按 part_size 上传所有完整 part，尾 part 留到 complete 前再上传。
func (st *ossTransferState) flushReadyParts(ctx context.Context, api ossMultipartAPI, bucket string, partSize int64) error {
	for int64(len(st.pendingPartBuffer)) >= partSize {
		if err := st.uploadPartN(ctx, api, bucket, int(partSize)); err != nil {
			return err
		}
	}
	return nil
}

func (st *ossTransferState) uploadPart(ctx context.Context, api ossMultipartAPI, bucket string) error {
	return st.uploadPartN(ctx, api, bucket, len(st.pendingPartBuffer))
}

func (st *ossTransferState) uploadPartN(ctx context.Context, api ossMultipartAPI, bucket string, n int) error {
	if n <= 0 {
		return nil
	}
	partPayload := append([]byte(nil), st.pendingPartBuffer[:n]...)
	partStart := st.uploadedEnd()
	objPart, err := api.PutObjectPart(ctx, bucket, st.objectKey, st.uploadID, st.nextPartNumber, bytes.NewReader(partPayload), int64(len(partPayload)), minio.PutObjectPartOptions{})
	if err != nil {
		return err
	}
	st.uploadedParts = append(st.uploadedParts, ossUploadedPart{partNumber: st.nextPartNumber, etag: objPart.ETag, start: partStart, end: partStart + int64(len(partPayload))})
	st.nextPartNumber++
	copy(st.pendingPartBuffer, st.pendingPartBuffer[n:])
	st.pendingPartBuffer = st.pendingPartBuffer[:len(st.pendingPartBuffer)-n]
	return nil
}

func (st *ossTransferState) uploadedEnd() int64 {
	if len(st.uploadedParts) == 0 {
		return 0
	}
	return st.uploadedParts[len(st.uploadedParts)-1].end
}

func (st *ossTransferState) addRange(start, end int64) {
	tmp := sftpTransferState{ranges: st.coveredRanges, coveredBytes: st.coveredBytes}
	tmp.addRange(start, end)
	st.coveredRanges = tmp.ranges
	st.coveredBytes = tmp.coveredBytes
}

func (st *ossTransferState) readyToComplete() bool {
	if !st.eofSeen || len(st.pendingSegments) != 0 {
		return false
	}
	if st.totalSize <= 0 {
		return len(st.coveredRanges) == 1
	}
	if st.coveredBytes < st.totalSize || st.nextOffset < st.totalSize {
		return false
	}
	return len(st.coveredRanges) == 1 && st.coveredRanges[0].start == 0 && st.coveredRanges[0].end >= st.totalSize
}

func (st *ossTransferState) completeParts() []minio.CompletePart {
	parts := make([]minio.CompletePart, 0, len(st.uploadedParts))
	for _, p := range st.uploadedParts {
		parts = append(parts, minio.CompletePart{PartNumber: p.partNumber, ETag: p.etag})
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].PartNumber < parts[j].PartNumber })
	return parts
}

// ossObjectKey 生成最终 object key，优先级固定为：
//  1. packet.Meta.TargetFilePath；
//  2. key_prefix + packet.Meta.FilePath；
//  3. key_prefix + packet.Meta.FileName。
//
// 所有路径都会先转成安全相对路径，拒绝空 key 与 .. 路径，避免把不安全来源路径直接拼接到 OSS。
func ossObjectKey(keyPrefix string, p *packet.Packet) (string, error) {
	if strings.TrimSpace(p.Meta.TargetFilePath) != "" {
		key, err := safeRelativePath(p.Meta.TargetFilePath)
		if err != nil {
			return "", fmt.Errorf("oss sender invalid target_file_path: %w", err)
		}
		return key, nil
	}
	src := strings.TrimSpace(p.Meta.FilePath)
	if src == "" {
		src = strings.TrimSpace(p.Meta.FileName)
	}
	if src == "" {
		return "", fmt.Errorf("oss sender cannot build empty object key")
	}
	rel, err := safeRelativePath(src)
	if err != nil {
		return "", fmt.Errorf("oss sender invalid source path: %w", err)
	}
	if strings.TrimSpace(p.Meta.TargetFileName) != "" {
		rel = applyTargetFileName(rel, p.Meta.TargetFileName)
	}
	prefix, err := safeOptionalPrefix(keyPrefix)
	if err != nil {
		return "", err
	}
	if prefix != "" {
		rel = prefix + "/" + rel
	}
	if rel == "" {
		return "", fmt.Errorf("oss sender cannot build empty object key")
	}
	return rel, nil
}

func safeOptionalPrefix(prefix string) (string, error) {
	if strings.TrimSpace(prefix) == "" {
		return "", nil
	}
	return safeRelativePath(prefix)
}

func minInt64(a, b int64) int {
	if a < b {
		return int(a)
	}
	return int(b)
}
