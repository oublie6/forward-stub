// sftp.go 实现 SFTP 文件发送端（接收 chunk 并远端组装落盘）。
package sender

import (
	"context"
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

type sftpTransferState struct {
	tempPath   string
	finalPath  string
	totalSize  int64
	eofSeen    bool
	writtenMax int64
}

type SFTPSender struct {
	name       string
	addr       string
	username   string
	password   string
	remoteDir  string
	tempSuffix string

	mu        sync.Mutex
	sshClient *ssh.Client
	sftpCli   *sftp.Client
	states    map[string]*sftpTransferState
}

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
		name:       name,
		addr:       strings.TrimSpace(sc.Remote),
		username:   strings.TrimSpace(sc.Username),
		password:   sc.Password,
		remoteDir:  strings.TrimSpace(sc.RemoteDir),
		tempSuffix: suffix,
		states:     make(map[string]*sftpTransferState),
	}
	if err := s.ensureConn(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *SFTPSender) Name() string { return s.name }
func (s *SFTPSender) Key() string  { return "sftp|" + s.addr + "|" + s.remoteDir }

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
	fileName := path.Base(strings.TrimSpace(p.Meta.FilePath))
	if fileName == "." || fileName == "/" || fileName == "" {
		fileName = transferID + ".bin"
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
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
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
