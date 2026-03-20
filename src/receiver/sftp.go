// sftp.go 实现 SFTP 文件接收端（按 chunk 读取并输出为 packet）。
package receiver

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"forward-stub/src/config"
	"forward-stub/src/logx"
	"forward-stub/src/packet"

	"github.com/pkg/sftp"
	"go.uber.org/zap/zapcore"
	"golang.org/x/crypto/ssh"
)

// SFTPReceiver 是基于轮询目录的 SFTP 接收端。
//
// 工作方式：
//  1. 按 poll_interval_sec 定时连接远端目录；
//  2. 对每个常规文件按 chunk 读取并转成 Packet(file_chunk)；
//  3. 通过 seen 指纹避免重复消费未变化文件。
type SFTPReceiver struct {
	// name 是 receiver 实例名（配置 key）。
	// 用法：用于日志打点、运行时映射与指标标签。
	name string
	// cfg 是 receiver 配置快照。
	// 用法：Start/scan 过程中读取轮询、目录、认证等参数。
	cfg config.ReceiverConfig

	// onPacket 是上游注入的投递回调。
	// 用法：每读到一个 chunk 就调用一次，把数据送入 task 流程。
	onPacket func(*packet.Packet)
	// matchKeyBuilder / matchKeyMode 是初始化阶段编译好的 match key 逻辑与模式。
	matchKeyBuilder sftpMatchKeyBuilder
	matchKeyMode    string

	// mu 保护 cancel/done/seen 等可变状态。
	mu sync.Mutex
	// cancel 用于请求停止后台轮询循环。
	cancel context.CancelFunc
	// done 在 Start 退出时关闭，用于 Stop 等待。
	done chan struct{}

	// stats 是流量统计计数器。
	// 用法：启用 info 日志级别时按周期输出吞吐指标。
	stats *logx.TrafficCounter
	// seen 记录已处理文件指纹（size+mtime）。
	// 用法：避免同一文件在下次轮询中被重复读取。
	seen map[string]string
}

// NewSFTPReceiver 构造并校验 SFTPReceiver。
// 需要 listen/username/password/remote_dir/host_key_fingerprint 五个核心字段。
func NewSFTPReceiver(name string, rc config.ReceiverConfig) (*SFTPReceiver, error) {
	if strings.TrimSpace(rc.Listen) == "" {
		return nil, fmt.Errorf("sftp receiver requires listen")
	}
	if strings.TrimSpace(rc.Username) == "" {
		return nil, fmt.Errorf("sftp receiver requires username")
	}
	if strings.TrimSpace(rc.Password) == "" {
		return nil, fmt.Errorf("sftp receiver requires password")
	}
	if strings.TrimSpace(rc.RemoteDir) == "" {
		return nil, fmt.Errorf("sftp receiver requires remote_dir")
	}
	if err := config.ValidateSSHHostKeyFingerprint(rc.HostKeyFingerprint); err != nil {
		return nil, fmt.Errorf("sftp receiver invalid host_key_fingerprint: %w", err)
	}
	builder, mode, err := compileSFTPMatchKeyBuilder(rc.MatchKey, rc.RemoteDir)
	if err != nil {
		return nil, err
	}
	return &SFTPReceiver{
		name:            name,
		cfg:             rc,
		seen:            make(map[string]string),
		matchKeyBuilder: builder,
		matchKeyMode:    mode,
	}, nil
}

// Name 返回 receiver 名称（配置 key）。
func (r *SFTPReceiver) Name() string { return r.name }

// Key 返回 receiver 去重键，用于 runtime 复用实例。
func (r *SFTPReceiver) Key() string {
	return "sftp|" + strings.TrimSpace(r.cfg.Listen) + "|" + strings.TrimSpace(r.cfg.RemoteDir)
}

// MatchKeyMode 返回 SFTP receiver 当前生效的 match key 模式。
func (r *SFTPReceiver) MatchKeyMode() string { return r.matchKeyMode }

// Start 启动轮询循环并持续产出 packet。
// 用法：通常由 runtime 调用一次；成功后会阻塞直到 ctx 取消或 Stop 被调用。
func (r *SFTPReceiver) Start(ctx context.Context, onPacket func(*packet.Packet)) error {
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
			"proto", "sftp",
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
			logx.L().Warnw("sftp receiver scan failed", "receiver", r.name, "error", err)
		}
		select {
		case <-rctx.Done():
			return nil
		case <-time.After(poll):
		}
	}
}

// Stop 请求停止并等待 Start 退出。
// 用法：热更新或进程退出时调用，确保后台 goroutine 与连接正确回收。
func (r *SFTPReceiver) Stop(ctx context.Context) error {
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

// scanOnce 执行一次目录扫描并顺序处理文件。
// 用法：每个轮询周期调用一次，按文件名排序保证处理顺序稳定。
func (r *SFTPReceiver) scanOnce(ctx context.Context) error {
	cli, scli, err := r.connect()
	if err != nil {
		return err
	}
	defer cli.Close()
	defer scli.Close()

	dir := strings.TrimSpace(r.cfg.RemoteDir)
	entries, err := scli.ReadDir(dir)
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, e := range entries {
		if !e.Mode().IsRegular() {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil
		}
		fp := path.Join(dir, e.Name())
		sig := fmt.Sprintf("%d-%d", e.Size(), e.ModTime().UnixNano())
		if r.isSeen(fp, sig) {
			continue
		}
		if err := r.streamFile(ctx, scli, fp, e.Size()); err != nil {
			return err
		}
		r.markSeen(fp, sig)
	}
	return nil
}

// streamFile 将单个文件按 chunk 转换为 file_chunk packet 并回调输出。
// 用法：会为每个 chunk 计算 checksum，并携带 offset/EOF 供 sender 重组。
func (r *SFTPReceiver) streamFile(ctx context.Context, scli *sftp.Client, filePath string, totalSize int64) error {
	f, err := scli.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	chunkSize := defaultInt(r.cfg.ChunkSize, 64*1024)
	if chunkSize < 1024 {
		chunkSize = 1024
	}
	buf := make([]byte, chunkSize)
	offset := int64(0)
	transferID := fmt.Sprintf("%s|%d", filePath, totalSize)
	matchKey := r.matchKeyBuilder(filePath)
	fileName := path.Base(filePath)

	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		n, err := f.Read(buf)
		if n > 0 {
			eof := err == io.EOF
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
						Proto:      packet.ProtoSFTP,
						Remote:     filePath,
						Local:      r.cfg.Listen,
						MatchKey:   matchKey,
						FileName:   fileName,
						FilePath:   filePath,
						TransferID: transferID,
						Offset:     offset,
						TotalSize:  totalSize,
						Checksum:   hex.EncodeToString(h[:]),
						EOF:        eof,
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

// connect 建立 SSH 与 SFTP 客户端连接。
// 用法：每轮扫描建立短连接，扫描结束后立即关闭以降低长期连接失效风险。
func (r *SFTPReceiver) connect() (*ssh.Client, *sftp.Client, error) {
	addr := strings.TrimSpace(r.cfg.Listen)
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return nil, nil, fmt.Errorf("invalid sftp listen addr %q: %w", addr, err)
	}
	sshCfg := &ssh.ClientConfig{
		User:            strings.TrimSpace(r.cfg.Username),
		Auth:            []ssh.AuthMethod{ssh.Password(r.cfg.Password)},
		HostKeyCallback: r.hostKeyCallback(),
		Timeout:         10 * time.Second,
	}
	cli, err := ssh.Dial("tcp", addr, sshCfg)
	if err != nil {
		return nil, nil, err
	}
	scli, err := sftp.NewClient(cli)
	if err != nil {
		_ = cli.Close()
		return nil, nil, err
	}
	return cli, scli, nil
}

func (r *SFTPReceiver) hostKeyCallback() ssh.HostKeyCallback {
	want := strings.TrimSpace(r.cfg.HostKeyFingerprint)
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		got := ssh.FingerprintSHA256(key)
		if subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1 {
			return nil
		}
		return fmt.Errorf("sftp host key mismatch for %s: got=%s want=%s", hostname, got, want)
	}
}

// isSeen 判断文件指纹是否已处理。
// 用法：在处理前调用，命中则跳过该文件。
func (r *SFTPReceiver) isSeen(fp, sig string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seen[fp] == sig
}

// markSeen 记录文件最新指纹，避免重复消费。
// 用法：文件完整 stream 成功后调用，确保失败文件可在下轮重试。
func (r *SFTPReceiver) markSeen(fp, sig string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen[fp] = sig
}

// defaultInt 返回 v（若 v<=0 则返回默认值 d）。
// 用法：统一处理配置字段“未设置或非法值”时的兜底行为。
func defaultInt(v, d int) int {
	if v <= 0 {
		return d
	}
	return v
}
