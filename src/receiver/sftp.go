// sftp.go 实现 SFTP 文件接收端（按 chunk 读取并输出为 packet）。
package receiver

import (
	"context"
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

type SFTPReceiver struct {
	name string
	cfg  config.ReceiverConfig

	onPacket func(*packet.Packet)

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}

	stats *logx.TrafficCounter
	seen  map[string]string
}

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
	return &SFTPReceiver{name: name, cfg: rc, seen: make(map[string]string)}, nil
}

func (r *SFTPReceiver) Name() string { return r.name }

func (r *SFTPReceiver) Key() string {
	return "sftp|" + strings.TrimSpace(r.cfg.Listen) + "|" + strings.TrimSpace(r.cfg.RemoteDir)
}

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
		if err := r.scanOnce(rctx); err != nil && logx.Enabled(zapcore.WarnLevel) {
			logx.L().Warnw("sftp receiver scan failed", "receiver", r.name, "error", err)
		}
		select {
		case <-rctx.Done():
			return nil
		case <-time.After(poll):
		}
	}
}

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
		if err := r.streamFile(ctx, scli, fp); err != nil {
			return err
		}
		r.markSeen(fp, sig)
	}
	return nil
}

func (r *SFTPReceiver) streamFile(ctx context.Context, scli *sftp.Client, filePath string) error {
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

	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		n, err := f.Read(buf)
		if n > 0 {
			payload, rel := packet.CopyFrom(buf[:n])
			if r.stats != nil {
				r.stats.AddBytes(n)
			}
			r.onPacket(&packet.Packet{
				Payload: payload,
				Meta: packet.Meta{
					Proto:  packet.ProtoSFTP,
					Remote: filePath,
					Local:  r.cfg.Listen,
				},
				ReleaseFn: rel,
			})
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (r *SFTPReceiver) connect() (*ssh.Client, *sftp.Client, error) {
	addr := strings.TrimSpace(r.cfg.Listen)
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return nil, nil, fmt.Errorf("invalid sftp listen addr %q: %w", addr, err)
	}
	sshCfg := &ssh.ClientConfig{
		User:            strings.TrimSpace(r.cfg.Username),
		Auth:            []ssh.AuthMethod{ssh.Password(r.cfg.Password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
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

func (r *SFTPReceiver) isSeen(fp, sig string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seen[fp] == sig
}

func (r *SFTPReceiver) markSeen(fp, sig string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen[fp] = sig
}

func defaultInt(v, d int) int {
	if v <= 0 {
		return d
	}
	return v
}
