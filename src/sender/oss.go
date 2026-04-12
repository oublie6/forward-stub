package sender

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	AbortMultipartUpload(ctx context.Context, bucket, object, uploadID string) error
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

func (m minioCoreAPI) AbortMultipartUpload(ctx context.Context, bucket, object, uploadID string) error {
	return m.core.AbortMultipartUpload(ctx, bucket, object, uploadID)
}

var errOSSPendingLimitExceeded = errors.New("oss sender pending out-of-order chunks exceed bounded limit")

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
	// mu 只保护单个 transfer 的状态机。OSSSender.mu 只保护 states map，
	// multipart upload / complete / notify 都不能放在 sender 级别锁内。
	mu       sync.Mutex
	initDone chan struct{}
	initErr  error

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
	uploading       bool
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

	st, creator, err := s.getOrCreateTransferState(ctx, transferID, p)
	if err != nil {
		return err
	}
	if !creator {
		<-st.initDone
		if st.initErr != nil {
			return st.initErr
		}
	}

	shouldDrive, err := st.acceptPacket(p, s.pendingLimit, s.partSize)
	if err != nil {
		if isOSSTerminalTransferError(err) {
			if abortErr := s.failTransfer(ctx, transferID, st); abortErr != nil {
				return errors.Join(err, abortErr)
			}
		}
		return err
	}
	if !shouldDrive {
		return nil
	}
	return s.driveOSSUpload(ctx, transferID, st, p)
}

func (s *OSSSender) getOrCreateTransferState(ctx context.Context, transferID string, p *packet.Packet) (*ossTransferState, bool, error) {
	s.mu.Lock()
	st, ok := s.states[transferID]
	if ok {
		s.mu.Unlock()
		return st, false, nil
	}
	st = &ossTransferState{
		initDone:        make(chan struct{}),
		totalSize:       p.Meta.TotalSize,
		pendingSegments: make(map[int64][]byte),
		nextPartNumber:  1,
	}
	s.states[transferID] = st
	s.mu.Unlock()

	var err error
	st.objectKey, err = ossObjectKey(s.keyPrefix, p)
	if err == nil {
		st.uploadID, err = s.api.NewMultipartUpload(ctx, s.bucket, st.objectKey, s.putOpts)
	}
	if err != nil {
		st.initErr = err
		s.mu.Lock()
		if s.states[transferID] == st {
			delete(s.states, transferID)
		}
		s.mu.Unlock()
		close(st.initDone)
		return st, true, err
	}
	st.pendingPartBuffer = make([]byte, 0, minInt64(s.partSize, 16<<20))
	close(st.initDone)
	return st, true, nil
}

func (st *ossTransferState) acceptPacket(p *packet.Packet, pendingLimit, partSize int64) (bool, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if p.Meta.TotalSize > 0 {
		st.totalSize = p.Meta.TotalSize
	}
	if p.Meta.EOF {
		st.eofSeen = true
	}
	st.addRange(p.Meta.Offset, p.Meta.Offset+int64(len(p.Payload)))
	if err := st.acceptChunk(p.Meta.Offset, p.Payload, pendingLimit); err != nil {
		return false, err
	}
	if st.uploading {
		return false, nil
	}
	if !st.hasUploadWorkLocked(partSize) {
		return false, nil
	}
	st.uploading = true
	return true, nil
}

func (s *OSSSender) driveOSSUpload(ctx context.Context, transferID string, st *ossTransferState, p *packet.Packet) error {
	for {
		op := st.nextUploadOperation(s.partSize)
		switch op.kind {
		case ossUploadOpNone:
			return nil
		case ossUploadOpPart:
			objPart, err := s.api.PutObjectPart(ctx, s.bucket, st.objectKey, st.uploadID, op.partNumber, bytes.NewReader(op.payload), int64(len(op.payload)), minio.PutObjectPartOptions{})
			if err != nil {
				st.finishPartUpload(op, "", err)
				return err
			}
			st.finishPartUpload(op, objPart.ETag, nil)
		case ossUploadOpComplete:
			if _, err := s.api.CompleteMultipartUpload(ctx, s.bucket, op.objectKey, op.uploadID, op.parts, s.putOpts); err != nil {
				st.finishCompleteUpload()
				return err
			}
			st.finishCompleteUpload()
			s.mu.Lock()
			if s.states[transferID] == st {
				delete(s.states, transferID)
			}
			s.mu.Unlock()
			if err := notifyFileReady(ctx, s.notifiers, ossFileReadyEvent(p, s.name, s.endpoint, s.bucket, op.objectKey)); err != nil {
				// TODO: 在这里接入通知 outbox / 持久化补发表。对象已经 complete 成功，不能再保留 multipart 未完成状态。
				logx.L().Errorw("OSS对象已complete成功，通知失败", "发送端", s.name, "bucket", s.bucket, "key", op.objectKey, "错误", err)
				return fmt.Errorf("oss sender object completed but notify failed: %w", err)
			}
			return nil
		}
	}
}

func isOSSTerminalTransferError(err error) bool {
	// Terminal failures mean the in-memory transfer state is no longer trustworthy.
	// Keeping it would make later chunks for the same transfer_id continue from a corrupted state.
	return errors.Is(err, errOSSPendingLimitExceeded)
}

func (s *OSSSender) failTransfer(ctx context.Context, transferID string, st *ossTransferState) error {
	s.mu.Lock()
	if s.states[transferID] == st {
		delete(s.states, transferID)
	}
	s.mu.Unlock()
	return s.abortTransfer(ctx, st)
}

type ossUploadOpKind uint8

const (
	ossUploadOpNone ossUploadOpKind = iota
	ossUploadOpPart
	ossUploadOpComplete
)

type ossUploadOp struct {
	kind       ossUploadOpKind
	partNumber int
	partStart  int64
	payload    []byte
	objectKey  string
	uploadID   string
	parts      []minio.CompletePart
}

func (st *ossTransferState) nextUploadOperation(partSize int64) ossUploadOp {
	st.mu.Lock()
	defer st.mu.Unlock()
	if int64(len(st.pendingPartBuffer)) >= partSize {
		return st.reservePartUploadLocked(int(partSize))
	}
	if st.readyToComplete() {
		if len(st.pendingPartBuffer) > 0 {
			return st.reservePartUploadLocked(len(st.pendingPartBuffer))
		}
		return ossUploadOp{kind: ossUploadOpComplete, objectKey: st.objectKey, uploadID: st.uploadID, parts: st.completeParts()}
	}
	st.uploading = false
	return ossUploadOp{kind: ossUploadOpNone}
}

func (st *ossTransferState) reservePartUploadLocked(n int) ossUploadOp {
	partPayload := append([]byte(nil), st.pendingPartBuffer[:n]...)
	partStart := st.uploadedEnd()
	copy(st.pendingPartBuffer, st.pendingPartBuffer[n:])
	st.pendingPartBuffer = st.pendingPartBuffer[:len(st.pendingPartBuffer)-n]
	return ossUploadOp{
		kind:       ossUploadOpPart,
		partNumber: st.nextPartNumber,
		partStart:  partStart,
		payload:    partPayload,
	}
}

func (st *ossTransferState) finishPartUpload(op ossUploadOp, etag string, uploadErr error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if uploadErr != nil {
		st.pendingPartBuffer = append(op.payload, st.pendingPartBuffer...)
		st.uploading = false
		return
	}
	st.uploadedParts = append(st.uploadedParts, ossUploadedPart{partNumber: op.partNumber, etag: etag, start: op.partStart, end: op.partStart + int64(len(op.payload))})
	st.nextPartNumber++
}

func (st *ossTransferState) finishCompleteUpload() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.uploading = false
}

func (st *ossTransferState) hasUploadWorkLocked(partSize int64) bool {
	return int64(len(st.pendingPartBuffer)) >= partSize || st.readyToComplete()
}

func (s *OSSSender) Close(ctx context.Context) error {
	s.mu.Lock()
	states := make([]*ossTransferState, 0, len(s.states))
	for _, st := range s.states {
		states = append(states, st)
	}
	s.states = make(map[string]*ossTransferState)
	s.mu.Unlock()

	var errs []error
	for _, st := range states {
		if err := s.abortTransfer(ctx, st); err != nil {
			errs = append(errs, err)
		}
	}
	for _, n := range s.notifiers {
		_ = n.Close(ctx)
	}
	return errors.Join(errs...)
}

func (s *OSSSender) abortTransfer(ctx context.Context, st *ossTransferState) error {
	if st == nil {
		return nil
	}
	if st.initDone != nil {
		<-st.initDone
	}
	if st.initErr != nil {
		return nil
	}
	st.mu.Lock()
	objectKey := st.objectKey
	uploadID := st.uploadID
	st.mu.Unlock()
	if objectKey == "" || uploadID == "" {
		return nil
	}
	if err := s.api.AbortMultipartUpload(ctx, s.bucket, objectKey, uploadID); err != nil {
		return fmt.Errorf("abort oss multipart upload object=%s upload_id=%s: %w", objectKey, uploadID, err)
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
		return fmt.Errorf("%w: pending=%d limit=%d", errOSSPendingLimitExceeded, st.pendingBytes, pendingLimit)
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
