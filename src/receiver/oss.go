package receiver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"forward-stub/src/config"
	"forward-stub/src/logx"
	"forward-stub/src/packet"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"go.uber.org/zap/zapcore"
)

// OSSReceiver 是基于轮询 bucket/prefix 的 OSS 接收端。
//
// 它按对象 key 稳定排序后逐个流式读取对象，输出 PayloadKindFileChunk。
// seen 表按 key 保存 size/mtime/etag 指纹：同名对象覆盖后指纹变化，会再次被处理。
// seen 使用固定容量 FIFO 淘汰最早记录，避免长期运行时随着历史 key 无界增长。
type OSSReceiver struct {
	name string
	cfg  config.ReceiverConfig
	cli  *minio.Client

	onPacket        func(*packet.Packet)
	matchKeyBuilder ossMatchKeyBuilder
	matchKeyMode    string

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	stats  *logx.TrafficCounter
	seen   map[string]string
	// seenOrder 记录 key 首次进入 seen 的顺序；超过 seenLimit 后淘汰最旧 key。
	// 这保留了最近窗口内的去重能力，同时把内存上界固定在简单可预期的范围。
	seenOrder []string
	seenLimit int
}

const defaultOSSReceiverSeenLimit = 10000

// NewOSSReceiver 构造 OSSReceiver 并编译 match key builder。
// endpoint/bucket/access_key/secret_key 必须完整；prefix 只影响轮询范围，不参与 match key 默认格式。
func NewOSSReceiver(name string, rc config.ReceiverConfig) (*OSSReceiver, error) {
	if strings.TrimSpace(rc.Endpoint) == "" {
		return nil, fmt.Errorf("oss receiver requires endpoint")
	}
	if strings.TrimSpace(rc.Bucket) == "" {
		return nil, fmt.Errorf("oss receiver requires bucket")
	}
	if strings.TrimSpace(rc.AccessKey) == "" || strings.TrimSpace(rc.SecretKey) == "" {
		return nil, fmt.Errorf("oss receiver requires access_key and secret_key")
	}
	opts := &minio.Options{
		Creds:  credentials.NewStaticV4(strings.TrimSpace(rc.AccessKey), strings.TrimSpace(rc.SecretKey), ""),
		Secure: rc.UseSSL,
		Region: strings.TrimSpace(rc.Region),
	}
	if rc.ForcePathStyle {
		opts.BucketLookup = minio.BucketLookupPath
	}
	cli, err := minio.New(strings.TrimSpace(rc.Endpoint), opts)
	if err != nil {
		return nil, err
	}
	builder, mode, err := compileOSSMatchKeyBuilder(rc.MatchKey, rc.Bucket)
	if err != nil {
		return nil, err
	}
	return &OSSReceiver{name: name, cfg: rc, cli: cli, seen: make(map[string]string), seenLimit: defaultOSSReceiverSeenLimit, matchKeyBuilder: builder, matchKeyMode: mode}, nil
}

func (r *OSSReceiver) Name() string { return r.name }

// Key 返回 receiver 去重键，用于 runtime 在热更新时判断是否可复用实例。
func (r *OSSReceiver) Key() string {
	return "oss|" + strings.TrimSpace(r.cfg.Endpoint) + "|" + strings.TrimSpace(r.cfg.Bucket) + "|" + strings.TrimSpace(r.cfg.Prefix)
}

// MatchKeyMode 返回初始化时编译生效的 match_key.mode，主要用于观测和测试。
func (r *OSSReceiver) MatchKeyMode() string { return r.matchKeyMode }

// Start 启动轮询循环，直到 ctx 取消或 Stop 被调用。
// 每轮 scanOnce 失败只记录告警并进入下一轮，避免临时 OSS 错误直接退出 receiver。
func (r *OSSReceiver) Start(ctx context.Context, onPacket func(*packet.Packet)) error {
	r.onPacket = onPacket

	r.mu.Lock()
	rctx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	r.done = make(chan struct{})
	if logx.Enabled(zapcore.InfoLevel) {
		r.stats = logx.AcquireTrafficCounter(
			"receiver traffic stats",
			"role", "receiver",
			"receiver", r.Name(),
			"receiver_key", r.Key(),
			"proto", "oss",
		)
	}
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		if r.stats != nil {
			r.stats.Close()
			r.stats = nil
		}
		if r.cancel != nil {
			r.cancel()
			r.cancel = nil
		}
		if r.done != nil {
			close(r.done)
			r.done = nil
		}
		r.mu.Unlock()
	}()

	poll := time.Duration(defaultInt(r.cfg.PollIntervalSec, 5)) * time.Second
	for {
		if err := r.scanOnce(rctx); err != nil {
			logx.L().Warnw("oss receiver scan failed", "receiver", r.name, "error", err)
		}
		select {
		case <-rctx.Done():
			return nil
		case <-time.After(poll):
		}
	}
}

// Stop 请求停止轮询并等待 Start 返回。
func (r *OSSReceiver) Stop(ctx context.Context) error {
	r.mu.Lock()
	cancel := r.cancel
	done := r.done
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// scanOnce 执行一次对象列表扫描。
// 目录占位对象、空 key、异常 size 会被跳过；对象按 key 排序以保持可重复处理顺序。
func (r *OSSReceiver) scanOnce(ctx context.Context) error {
	bucket := strings.TrimSpace(r.cfg.Bucket)
	opts := minio.ListObjectsOptions{Prefix: strings.TrimSpace(r.cfg.Prefix), Recursive: true, WithMetadata: true}
	var objects []minio.ObjectInfo
	for obj := range r.cli.ListObjects(ctx, bucket, opts) {
		if obj.Err != nil {
			return obj.Err
		}
		if obj.Key == "" || strings.HasSuffix(obj.Key, "/") || obj.Size < 0 {
			continue
		}
		objects = append(objects, obj)
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].Key < objects[j].Key })
	for _, obj := range objects {
		if err := ctx.Err(); err != nil {
			return nil
		}
		sig := fmt.Sprintf("%s|%d|%d|%s", obj.Key, obj.Size, obj.LastModified.UnixNano(), obj.ETag)
		if r.isSeen(obj.Key, sig) {
			continue
		}
		if err := r.streamObject(ctx, obj); err != nil {
			return err
		}
		r.markSeen(obj.Key, sig)
	}
	return nil
}

// streamObject 将单个 OSS object 按 chunk 转成 packet。
// chunk_size 未配置时为 64 KiB；显式配置小于 1024 会被抬升，避免过小 chunk 放大调度开销。
func (r *OSSReceiver) streamObject(ctx context.Context, obj minio.ObjectInfo) error {
	reader, err := r.cli.GetObject(ctx, strings.TrimSpace(r.cfg.Bucket), obj.Key, minio.GetObjectOptions{})
	if err != nil {
		return err
	}
	defer reader.Close()

	chunkSize := defaultInt(r.cfg.ChunkSize, 64*1024)
	if chunkSize < 1024 {
		chunkSize = 1024
	}
	buf := make([]byte, chunkSize)
	offset := int64(0)
	transferID := fmt.Sprintf("%s|%s|%d|%s", strings.TrimSpace(r.cfg.Bucket), obj.Key, obj.Size, obj.ETag)
	matchKey := r.matchKeyBuilder(obj.Key)
	fileName := path.Base(obj.Key)

	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		n, err := reader.Read(buf)
		if n > 0 {
			eof := fileChunkEOF(offset, n, obj.Size, err)
			payload, rel := packet.CopyFrom(buf[:n])
			if r.stats != nil {
				r.stats.AddBytes(n)
			}
			h := sha256.Sum256(buf[:n])
			r.onPacket(&packet.Packet{
				Envelope: packet.Envelope{
					Kind:    packet.PayloadKindFileChunk,
					Payload: payload,
					Meta: packet.Meta{
						Proto:        packet.ProtoOSS,
						ReceiverName: r.name,
						Remote:       obj.Key,
						Local:        strings.TrimSpace(r.cfg.Bucket) + "@" + strings.TrimSpace(r.cfg.Endpoint),
						MatchKey:     matchKey,
						FileName:     fileName,
						FilePath:     obj.Key,
						TransferID:   transferID,
						Offset:       offset,
						TotalSize:    obj.Size,
						Checksum:     hex.EncodeToString(h[:]),
						EOF:          eof,
					},
				},
				ReleaseFn: rel,
			})
			offset += int64(n)
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (r *OSSReceiver) isSeen(key, sig string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seen[key] == sig
}

func (r *OSSReceiver) markSeen(key, sig string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seen == nil {
		r.seen = make(map[string]string)
	}
	if _, ok := r.seen[key]; !ok {
		r.seenOrder = append(r.seenOrder, key)
	}
	r.seen[key] = sig
	limit := r.seenLimit
	if limit <= 0 {
		limit = defaultOSSReceiverSeenLimit
	}
	for len(r.seen) > limit && len(r.seenOrder) > 0 {
		oldest := r.seenOrder[0]
		copy(r.seenOrder, r.seenOrder[1:])
		r.seenOrder = r.seenOrder[:len(r.seenOrder)-1]
		delete(r.seen, oldest)
	}
}
