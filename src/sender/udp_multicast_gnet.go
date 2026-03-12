// udp_multicast_gnet.go 实现 UDP 组播发送端。
package sender

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"forward-stub/src/packet"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// UDPMulticastSender：方向A（最通用）实现
// - 使用 net.DialUDP 绑定固定源端口（local_ip:local_port）并发送到组播地址 group
// - 组播相关 socket option 使用 x/net 的 ipv4/ipv6 PacketConn 设置（跨平台）
// - 设置 SO_REUSEADDR以允许多 sender 复用同一 local port
type UDPMulticastSender struct {
	name      string
	group     *net.UDPAddr
	local     *net.UDPAddr
	ifaceName string
	ttl       int
	loop      bool

	concurrency int
	locks       []sync.Mutex
	conns       []atomic.Pointer[net.UDPConn]
	nextIdx     atomic.Uint64
}

// NewUDPMulticastSender 负责该函数对应的核心逻辑，详见实现细节。
func NewUDPMulticastSender(name, localIP string, localPort int, group string, ifaceName string, ttl int, loop bool, concurrency int) (*UDPMulticastSender, error) {
	gaddr, err := net.ResolveUDPAddr("udp", group)
	if err != nil {
		return nil, err
	}
	if localIP == "" {
		localIP = "0.0.0.0"
	}
	if ttl <= 0 {
		ttl = 1
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	laddr := &net.UDPAddr{IP: net.ParseIP(localIP), Port: localPort}

	s := &UDPMulticastSender{
		name:        name,
		group:       gaddr,
		local:       laddr,
		ifaceName:   ifaceName,
		ttl:         ttl,
		loop:        loop,
		concurrency: concurrency,
		locks:       make([]sync.Mutex, concurrency),
		conns:       make([]atomic.Pointer[net.UDPConn], concurrency),
	}
	for i := 0; i < s.concurrency; i++ {
		if err := s.ensureConn(i); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Name 负责该函数对应的核心逻辑，详见实现细节。
func (s *UDPMulticastSender) Name() string { return s.name }

// Key 负责该函数对应的核心逻辑，详见实现细节。
func (s *UDPMulticastSender) Key() string {
	return fmt.Sprintf("udp_multicast|%s->%s", s.local.String(), s.group.String())
}

// Send 负责该函数对应的核心逻辑，详见实现细节。
func (s *UDPMulticastSender) Send(ctx context.Context, p *packet.Packet) error {
	idx := s.pickShard()
	c := s.conns[idx].Load()
	if c == nil {
		var err error
		c, err = s.getConn(idx)
		if err != nil {
			return err
		}
	}
	_, err := c.Write(p.Payload)
	return err
}

// Close 负责该函数对应的核心逻辑，详见实现细节。
func (s *UDPMulticastSender) Close(ctx context.Context) error {
	for i := 0; i < s.concurrency; i++ {
		s.locks[i].Lock()
		if c := s.conns[i].Load(); c != nil {
			_ = c.Close()
			s.conns[i].Store(nil)
		}
		s.locks[i].Unlock()
	}
	return nil
}

// getConn 负责该函数对应的核心逻辑，详见实现细节。
func (s *UDPMulticastSender) getConn(idx int) (*net.UDPConn, error) {
	s.locks[idx].Lock()
	defer s.locks[idx].Unlock()
	if c := s.conns[idx].Load(); c != nil {
		return c, nil
	}
	if err := s.ensureConnLocked(idx); err != nil {
		return nil, err
	}
	return s.conns[idx].Load(), nil
}

// ensureConn 负责该函数对应的核心逻辑，详见实现细节。
func (s *UDPMulticastSender) ensureConn(idx int) error {
	s.locks[idx].Lock()
	defer s.locks[idx].Unlock()
	return s.ensureConnLocked(idx)
}

// ensureConnLocked 负责该函数对应的核心逻辑，详见实现细节。
func (s *UDPMulticastSender) ensureConnLocked(idx int) error {
	if s.conns[idx].Load() != nil {
		return nil
	}
	c, err := dialUDPWithReuse(context.Background(), s.local, s.group)
	if err != nil {
		return err
	}
	_ = c.SetWriteBuffer(4 << 20)

	// Multicast options via x/net
	if s.group.IP != nil && s.group.IP.To4() != nil {
		pc := ipv4.NewPacketConn(c)
		_ = pc.SetMulticastTTL(s.ttl)
		_ = pc.SetMulticastLoopback(s.loop)
		if s.ifaceName != "" {
			ifi, err := net.InterfaceByName(s.ifaceName)
			if err != nil {
				_ = c.Close()
				return err
			}
			_ = pc.SetMulticastInterface(ifi)
		}
	} else {
		pc := ipv6.NewPacketConn(c)
		_ = pc.SetMulticastHopLimit(s.ttl)
		_ = pc.SetMulticastLoopback(s.loop)
		if s.ifaceName != "" {
			ifi, err := net.InterfaceByName(s.ifaceName)
			if err != nil {
				_ = c.Close()
				return err
			}
			_ = pc.SetMulticastInterface(ifi)
		}
	}

	s.conns[idx].Store(c)
	return nil
}

func (s *UDPMulticastSender) pickShard() int {
	return nextShardIndex(&s.nextIdx, s.concurrency)
}
