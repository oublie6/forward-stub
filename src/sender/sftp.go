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
	"time"

	"forward-stub/src/config"
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
	// writtenMax 是已写入的最大末尾偏移。
	// 用法：支持乱序 chunk 到达时估算当前写入进度。
	writtenMax int64
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

	// mu 串行化 Send 与连接状态变更，避免并发写冲突。
	mu sync.Mutex
	// sshClient/sftpCli 是复用的底层连接。
	// 用法：ensureConn 保证可用，Close 统一释放。
	sshClient *ssh.Client
	sftpCli   *sftp.Client
	// states 保存 transfer_id 到组装状态的映射。
	// 用法：跨多次 Send 追踪同一文件的写入进度。
	states map[string]*sftpTransferState
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
		states:             make(map[string]*sftpTransferState),
	}
	if err := s.ensureConn(); err != nil {
		return nil, err
	}
	return s, nil
}

// Name 返回 sender 名称（配置 key）。
func (s *SFTPSender) Name() string { return s.name }

// Key 返回 sender 去重键，用于 runtime 复用实例。
func (s *SFTPSender) Key() string { return "sftp|" + s.addr + "|" + s.remoteDir }

// Send 接收一个 packet 并写入远端临时文件。
// 当满足完成条件（EOF + 写满 total_size）时，会原子 rename 为最终文件。
// 用法：推荐只投递 PayloadKindFileChunk 数据；若无 transfer_id 会自动生成临时会话。
func (s *SFTPSender) Send(ctx context.Context, p *packet.Packet) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureConn(); err != nil {
		return err
	}

	transferID := strings.TrimSpace(p.Meta.TransferID)
	if transferID == "" {
		transferID = fmt.Sprintf("stream-%d", time.Now().UnixNano())
	}
	fileName := strings.TrimSpace(p.Meta.FileName)
	if fileName == "" {
		fileName = path.Base(strings.TrimSpace(p.Meta.FilePath))
	}
	if fileName == "." || fileName == "/" || fileName == "" {
		fileName = transferID + ".bin"
	}

	if want := strings.TrimSpace(p.Meta.Checksum); want != "" {
		h := sha256.Sum256(p.Payload)
		if got := hex.EncodeToString(h[:]); !strings.EqualFold(got, want) {
			return fmt.Errorf("sftp sender checksum mismatch: got=%s want=%s transfer=%s", got, want, transferID)
		}
	}
	st, ok := s.states[transferID]
	if !ok {
		finalPath := path.Join(s.remoteDir, fileName)
		st = &sftpTransferState{
			finalPath: finalPath,
			tempPath:  finalPath + s.tempSuffix,
			totalSize: p.Meta.TotalSize,
		}
		s.states[transferID] = st
	}
	if p.Meta.TotalSize > 0 {
		st.totalSize = p.Meta.TotalSize
	}

	f, err := s.sftpCli.OpenFile(st.tempPath, os.O_WRONLY|os.O_CREATE)
	if err != nil {
		return err
	}
	if _, err = f.WriteAt(p.Payload, p.Meta.Offset); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	end := p.Meta.Offset + int64(len(p.Payload))
	if end > st.writtenMax {
		st.writtenMax = end
	}
	if p.Meta.EOF {
		st.eofSeen = true
	}

	if st.eofSeen && (st.totalSize <= 0 || st.writtenMax >= st.totalSize) {
		_ = s.sftpCli.Remove(st.finalPath)
		if err := s.sftpCli.Rename(st.tempPath, st.finalPath); err != nil {
			return err
		}
		delete(s.states, transferID)
	}
	return nil
}

// Close 关闭底层 SFTP/SSH 连接并释放资源。
// 用法：在 runtime 下线 sender 或进程退出时调用，避免连接泄漏。
func (s *SFTPSender) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sftpCli != nil {
		_ = s.sftpCli.Close()
		s.sftpCli = nil
	}
	if s.sshClient != nil {
		_ = s.sshClient.Close()
		s.sshClient = nil
	}
	return nil
}

// ensureConn 确保底层连接可用，不可用时执行重连。
// 用法：由 Send/构造阶段调用，调用方无需显式管理重连逻辑。
func (s *SFTPSender) ensureConn() error {
	if s.sftpCli != nil && s.sshClient != nil {
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
	s.sshClient = cli
	s.sftpCli = scli
	return nil
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
