// sftp.go 实现 SFTP 文件发送端（接收 chunk 并远端组装落盘）。
package sender

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"forward-stub/src/config"
	"forward-stub/src/logx"
	"forward-stub/src/packet"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// sftpTransferState 保存单个 transfer_id 在发送端的组装状态。
type sftpTransferState struct {
	// tempPath 是远端临时文件路径。
	// 用法：所有 chunk 先写入该路径，完成后再 rename 提交。
	tempPath string
	// finalPath 是远端最终文件路径。
	// 用法：达到提交条件后将 tempPath 原子重命名到该路径。
	finalPath string
	// totalSize 是期望文件总大小。
	// 用法：与 writtenMax、eofSeen 一起判断是否可安全提交。
	totalSize int64
	// eofSeen 表示是否已收到结束分块。
	// 用法：避免仅凭字节数达到阈值就过早提交。
	eofSeen bool
	// coveredBytes 是当前已确认无空洞覆盖的总字节数。
	coveredBytes int64
	// ranges 保存当前已收到的无重叠区间，用于判断是否存在空洞。
	ranges []fileRange
}

type fileRange struct {
	start int64
	end   int64
}

// SFTPSender 是按分块写入并最终 rename 提交的 SFTP 发送端。
//
// 使用方式：
//  1. task 将 file_chunk packet 传入 Send；
//  2. sender 依据 transfer_id + offset 写入临时文件；
//  3. 接收到 EOF 且写满 total_size 后 rename 为正式文件。
type SFTPSender struct {
	// name 是 sender 实例名（配置 key）。
	// 用法：用于 runtime 映射与日志定位。
	name string
	// addr 是远端 SFTP 服务地址（host:port）。
	// 用法：ensureConn 会校验格式并据此建立 SSH 连接。
	addr string
	// username/password 是远端认证凭据。
	// 用法：用于 SSH 密码认证，建议由密钥管理系统注入。
	username string
	password string
	// remoteDir 是最终文件写入目录。
	// 用法：连接建立时会尝试 MkdirAll，确保目录存在。
	remoteDir string
	// tempSuffix 是临时文件后缀。
	// 用法：下游可据此过滤未完成文件。
	tempSuffix string
	// hostKeyFingerprint 是预期服务端主机公钥指纹（SSH SHA256）。
	// 用法：用于 SSH 握手阶段校验服务端身份，抵御中间人攻击。
	hostKeyFingerprint string
	concurrency        int
	shardMask          int

	// locks 串行化每个分片内 Send 与连接状态变更，避免并发写冲突。
	locks []sync.Mutex
	// sshClients/sftpClis 是按分片复用的底层连接。
	// 用法：ensureConn 保证可用，Close 统一释放。
	sshClients []*ssh.Client
	sftpClis   []*sftp.Client
	connReady  []atomic.Bool
	// states 保存每个分片 transfer_id 到组装状态的映射。
	// 用法：跨多次 Send 追踪同一文件的写入进度。
	states []map[string]*sftpTransferState
	// notifiers 是文件 rename 成功后的后置通知动作。
	notifiers []FileReadyNotifier

	assignMu      sync.Mutex
	transferShard map[string]int
	nextIdx       atomic.Uint64
}

// NewSFTPSender 构造并校验 SFTPSender。
// 用法：创建时会主动探测连接；失败则返回错误避免 runtime 挂入不可用 sender。
func NewSFTPSender(name string, sc config.SenderConfig) (*SFTPSender, error) {
	if strings.TrimSpace(sc.Remote) == "" {
		return nil, fmt.Errorf("sftp sender requires remote")
	}
	if strings.TrimSpace(sc.Username) == "" || strings.TrimSpace(sc.Password) == "" {
		return nil, fmt.Errorf("sftp sender requires username and password")
	}
	if strings.TrimSpace(sc.RemoteDir) == "" {
		return nil, fmt.Errorf("sftp sender requires remote_dir")
	}
	if err := config.ValidateSSHHostKeyFingerprint(sc.HostKeyFingerprint); err != nil {
		return nil, fmt.Errorf("sftp sender invalid host_key_fingerprint: %w", err)
	}
	notifiers, err := buildFileReadyNotifiers(sc.NotifyOnSuccess)
	if err != nil {
		return nil, err
	}
	suffix := strings.TrimSpace(sc.TempSuffix)
	if suffix == "" {
		suffix = ".part"
	}
	s := &SFTPSender{
		name:               name,
		addr:               strings.TrimSpace(sc.Remote),
		username:           strings.TrimSpace(sc.Username),
		password:           sc.Password,
		remoteDir:          strings.TrimSpace(sc.RemoteDir),
		tempSuffix:         suffix,
		hostKeyFingerprint: strings.TrimSpace(sc.HostKeyFingerprint),
		concurrency:        config.DefaultSenderConcurrency,
		notifiers:          notifiers,
	}
	if sc.Concurrency > 0 {
		s.concurrency = sc.Concurrency
	}
	s.shardMask = s.concurrency - 1
	s.locks = make([]sync.Mutex, s.concurrency)
	s.sshClients = make([]*ssh.Client, s.concurrency)
	s.sftpClis = make([]*sftp.Client, s.concurrency)
	s.connReady = make([]atomic.Bool, s.concurrency)
	s.states = make([]map[string]*sftpTransferState, s.concurrency)
	s.transferShard = make(map[string]int)
	for i := 0; i < s.concurrency; i++ {
		s.states[i] = make(map[string]*sftpTransferState)
		if err := s.ensureConn(i); err != nil {
			_ = s.Close(context.Background())
			return nil, err
		}
	}
	return s, nil
}

// Name 返回 sender 名称（配置 key）。
func (s *SFTPSender) Name() string { return s.name }

// Key 返回 sender 去重键，用于 runtime 复用实例。
func (s *SFTPSender) Key() string { return "sftp|" + s.addr + "|" + s.remoteDir }

// Send 接收一个 packet 并写入远端临时文件。
// 当满足完成条件（EOF + 写满 total_size）时，会原子 rename 为最终文件。
// 用法：推荐只投递 PayloadKindFileChunk 数据；file_chunk 必须携带稳定 transfer_id。
func (s *SFTPSender) Send(ctx context.Context, p *packet.Packet) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	transferID := strings.TrimSpace(p.Meta.TransferID)
	if transferID == "" {
		if p.Kind == packet.PayloadKindFileChunk {
			return fmt.Errorf("sftp sender requires transfer_id for file_chunk payload")
		}
		return fmt.Errorf("sftp sender requires transfer_id")
	}
	idx := s.pickShard(transferID)
	if !s.connReady[idx].Load() {
		s.locks[idx].Lock()
		err := s.ensureConnLocked(idx)
		s.locks[idx].Unlock()
		if err != nil {
			return err
		}
	}
	s.locks[idx].Lock()
	readyEvent, err := s.sendLocked(idx, transferID, p)
	s.locks[idx].Unlock()
	if err != nil {
		return err
	}
	if readyEvent != nil {
		if err := notifyFileReady(ctx, s.notifiers, *readyEvent); err != nil {
			// TODO: 在这里接入通知 outbox / 持久化补发表。文件已经提交成功，不能再保留未完成传输状态。
			logx.L().Errorw("SFTP文件已提交成功，通知失败", "发送端", s.name, "目标路径", readyEvent.FetchPath, "错误", err)
			return fmt.Errorf("sftp sender file committed but notify failed: %w", err)
		}
	}
	return nil
}

func (s *SFTPSender) sendLocked(idx int, transferID string, p *packet.Packet) (*FileReadyEvent, error) {
	if s.sftpClis[idx] == nil || s.sshClients[idx] == nil {
		if err := s.ensureConnLocked(idx); err != nil {
			return nil, err
		}
	}
	if want := strings.TrimSpace(p.Meta.Checksum); want != "" {
		h := sha256.Sum256(p.Payload)
		if got := hex.EncodeToString(h[:]); !strings.EqualFold(got, want) {
			return nil, fmt.Errorf("sftp sender checksum mismatch: got=%s want=%s transfer=%s", got, want, transferID)
		}
	}
	st, ok := s.states[idx][transferID]
	if !ok {
		finalPath, err := sftpFinalPath(s.remoteDir, p, transferID)
		if err != nil {
			return nil, fmt.Errorf("sftp sender build final path failed: %w", err)
		}
		if err := s.sftpClis[idx].MkdirAll(path.Dir(finalPath)); err != nil {
			s.invalidateConnLocked(idx)
			return nil, err
		}
		st = &sftpTransferState{
			finalPath: finalPath,
			tempPath:  finalPath + s.tempSuffix,
			totalSize: p.Meta.TotalSize,
		}
		s.states[idx][transferID] = st
	}
	if p.Meta.TotalSize > 0 {
		st.totalSize = p.Meta.TotalSize
	}

	f, err := s.sftpClis[idx].OpenFile(st.tempPath, os.O_WRONLY|os.O_CREATE)
	if err != nil {
		s.invalidateConnLocked(idx)
		return nil, err
	}
	if _, err = f.WriteAt(p.Payload, p.Meta.Offset); err != nil {
		_ = f.Close()
		s.invalidateConnLocked(idx)
		return nil, err
	}
	if err = f.Close(); err != nil {
		s.invalidateConnLocked(idx)
		return nil, err
	}
	st.addRange(p.Meta.Offset, p.Meta.Offset+int64(len(p.Payload)))
	if p.Meta.EOF {
		st.eofSeen = true
	}

	if st.readyToCommit() {
		_ = s.sftpClis[idx].Remove(st.finalPath)
		if err := s.sftpClis[idx].Rename(st.tempPath, st.finalPath); err != nil {
			s.invalidateConnLocked(idx)
			return nil, err
		}
		finalPath := st.finalPath
		s.cleanupCommittedTransferLocked(idx, transferID)
		event := sftpFileReadyEvent(p, s.name, s.addr, finalPath)
		return &event, nil
	}
	return nil, nil
}

// Close 关闭底层 SFTP/SSH 连接并释放资源。
// 用法：在 runtime 下线 sender 或进程退出时调用，避免连接泄漏。
func (s *SFTPSender) Close(ctx context.Context) error {
	for i := 0; i < s.concurrency; i++ {
		s.locks[i].Lock()
		s.invalidateConnLocked(i)
		// Close 只能证明本进程内存状态失效，不能证明远端 .part 一定由本生命周期创建。
		// 因此这里清空状态但不批量删除远端临时文件，避免误删其他实例或旧会话仍在写的对象。
		if s.states[i] != nil {
			s.states[i] = make(map[string]*sftpTransferState)
		}
		s.locks[i].Unlock()
	}
	s.assignMu.Lock()
	s.transferShard = make(map[string]int)
	s.assignMu.Unlock()
	for _, n := range s.notifiers {
		_ = n.Close(ctx)
	}
	return nil
}

// ensureConn 确保底层连接可用，不可用时执行重连。
// 用法：由 Send/构造阶段调用，调用方无需显式管理重连逻辑。
func (s *SFTPSender) ensureConn(idx int) error {
	s.locks[idx].Lock()
	defer s.locks[idx].Unlock()
	return s.ensureConnLocked(idx)
}

func (s *SFTPSender) ensureConnLocked(idx int) error {
	if s.sftpClis[idx] != nil && s.sshClients[idx] != nil {
		return nil
	}
	if _, _, err := net.SplitHostPort(s.addr); err != nil {
		return fmt.Errorf("invalid sftp remote addr %q: %w", s.addr, err)
	}
	sshCfg := &ssh.ClientConfig{
		User:            s.username,
		Auth:            []ssh.AuthMethod{ssh.Password(s.password)},
		HostKeyCallback: s.hostKeyCallback(),
		Timeout:         10 * time.Second,
	}
	cli, err := ssh.Dial("tcp", s.addr, sshCfg)
	if err != nil {
		return err
	}
	scli, err := sftp.NewClient(cli)
	if err != nil {
		_ = cli.Close()
		return err
	}
	if err := scli.MkdirAll(s.remoteDir); err != nil {
		_ = scli.Close()
		_ = cli.Close()
		return err
	}
	s.sshClients[idx] = cli
	s.sftpClis[idx] = scli
	s.connReady[idx].Store(true)
	return nil
}

func (s *SFTPSender) invalidateConnLocked(idx int) {
	if s.sftpClis[idx] != nil {
		_ = s.sftpClis[idx].Close()
		s.sftpClis[idx] = nil
	}
	if s.sshClients[idx] != nil {
		_ = s.sshClients[idx].Close()
		s.sshClients[idx] = nil
	}
	s.connReady[idx].Store(false)
}

func (s *SFTPSender) pickShard(transferID string) int {
	if s.concurrency <= 1 {
		return 0
	}
	s.assignMu.Lock()
	defer s.assignMu.Unlock()
	if idx, ok := s.transferShard[transferID]; ok {
		return idx
	}
	idx := nextShardIndex(&s.nextIdx, s.shardMask)
	s.transferShard[transferID] = idx
	return idx
}

func (s *SFTPSender) cleanupCommittedTransferLocked(idx int, transferID string) {
	delete(s.states[idx], transferID)
	s.assignMu.Lock()
	delete(s.transferShard, transferID)
	s.assignMu.Unlock()
}

func (s *SFTPSender) hostKeyCallback() ssh.HostKeyCallback {
	want := strings.TrimSpace(s.hostKeyFingerprint)
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		got := ssh.FingerprintSHA256(key)
		if subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1 {
			return nil
		}
		return fmt.Errorf("sftp host key mismatch for %s: got=%s want=%s", hostname, got, want)
	}
}

func (st *sftpTransferState) addRange(start, end int64) {
	if end <= start {
		return
	}
	newRange := fileRange{start: start, end: end}
	if len(st.ranges) == 0 {
		st.ranges = append(st.ranges, newRange)
		st.coveredBytes += end - start
		return
	}

	merged := make([]fileRange, 0, len(st.ranges)+1)
	inserted := false
	covered := int64(0)
	for _, cur := range st.ranges {
		switch {
		case cur.end < newRange.start:
			merged = append(merged, cur)
		case newRange.end < cur.start:
			if !inserted {
				merged = append(merged, newRange)
				inserted = true
			}
			merged = append(merged, cur)
		default:
			if cur.start < newRange.start {
				newRange.start = cur.start
			}
			if cur.end > newRange.end {
				newRange.end = cur.end
			}
		}
	}
	if !inserted {
		merged = append(merged, newRange)
	}
	for _, r := range merged {
		covered += r.end - r.start
	}
	st.ranges = merged
	st.coveredBytes = covered
}

func (st *sftpTransferState) readyToCommit() bool {
	if !st.eofSeen {
		return false
	}
	if st.totalSize <= 0 {
		return len(st.ranges) == 1
	}
	if st.coveredBytes < st.totalSize {
		return false
	}
	return len(st.ranges) == 1 && st.ranges[0].start == 0 && st.ranges[0].end >= st.totalSize
}
