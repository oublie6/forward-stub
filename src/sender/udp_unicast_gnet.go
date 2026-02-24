// udp_unicast_gnet.go 实现 UDP 单播发送端。
package sender

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"forword-stub/src/packet"
)

// UDPUnicastSender：方向A（最通用）实现
// - 仅使用 net.DialUDP 绑定固定源端口（local_ip:local_port）并发送到 remote
// - 不设置 SO_REUSEPORT / SO_REUSEADDR（跨平台不做 syscall 分支）
// - 保证“同一个 sender = 同一个 socket”，天然满足“单 socket 内保序”
type UDPUnicastSender struct {
	name   string
	remote *net.UDPAddr
	local  *net.UDPAddr

	mu   sync.Mutex
	conn atomic.Pointer[net.UDPConn]
}

func NewUDPUnicastSender(name, localIP string, localPort int, remote string) (*UDPUnicastSender, error) {
	raddr, err := net.ResolveUDPAddr("udp", remote)
	if err != nil {
		return nil, err
	}
	if localIP == "" {
		localIP = "0.0.0.0"
	}
	laddr := &net.UDPAddr{IP: net.ParseIP(localIP), Port: localPort}

	s := &UDPUnicastSender{
		name:   name,
		remote: raddr,
		local:  laddr,
	}
	if err := s.ensureConn(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *UDPUnicastSender) Name() string { return s.name }
func (s *UDPUnicastSender) Key() string {
	return fmt.Sprintf("udp_unicast|%s->%s", s.local.String(), s.remote.String())
}

func (s *UDPUnicastSender) Send(ctx context.Context, p *packet.Packet) error {
	c := s.conn.Load()
	if c == nil {
		var err error
		c, err = s.getConn()
		if err != nil {
			return err
		}
	}
	_, err := c.Write(p.Payload)
	return err
}

func (s *UDPUnicastSender) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.conn.Load(); c != nil {
		_ = c.Close()
		s.conn.Store(nil)
	}
	return nil
}

func (s *UDPUnicastSender) getConn() (*net.UDPConn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.conn.Load(); c != nil {
		return c, nil
	}
	if err := s.ensureConnLocked(); err != nil {
		return nil, err
	}
	return s.conn.Load(), nil
}

func (s *UDPUnicastSender) ensureConn() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ensureConnLocked()
}

func (s *UDPUnicastSender) ensureConnLocked() error {
	if s.conn.Load() != nil {
		return nil
	}
	c, err := net.DialUDP("udp", s.local, s.remote)
	if err != nil {
		return err
	}
	_ = c.SetWriteBuffer(4 << 20)
	s.conn.Store(c)
	return nil
}
