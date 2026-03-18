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

// UDPMulticastSender 负责把处理后的 packet 发送到 UDP 组播地址。
//
// 设计说明：
//   - 每个 sender 维护固定数量的 shard socket，以在并发发送和 socket 复用之间折中；
//   - sender 位于 task 之后，不参与 selector 决策；
//   - shard 选择只影响发送并发，不改变单个 socket 内的报文顺序。
type UDPMulticastSender struct {
	name             string
	group            *net.UDPAddr
	local            *net.UDPAddr
	ifaceName        string
	ttl              int
	loop             bool
	socketSendBuffer int

	concurrency int
	shardMask   int
	locks       []sync.Mutex
	conns       []atomic.Pointer[net.UDPConn]
	nextIdx     atomic.Uint64
}

// NewUDPMulticastSender 创建并预热组播 sender 需要的 socket 分片。
func NewUDPMulticastSender(name, localIP string, localPort int, group string, ifaceName string, ttl int, loop bool, socketSendBuffer, concurrency int) (*UDPMulticastSender, error) {
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
		name:             name,
		group:            gaddr,
		local:            laddr,
		ifaceName:        ifaceName,
		ttl:              ttl,
		loop:             loop,
		socketSendBuffer: socketSendBuffer,
		concurrency:      concurrency,
		shardMask:        concurrency - 1,
		locks:            make([]sync.Mutex, concurrency),
		conns:            make([]atomic.Pointer[net.UDPConn], concurrency),
	}
	for i := 0; i < s.concurrency; i++ {
		if err := s.ensureConn(i); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Name 返回 sender 的配置名称。
func (s *UDPMulticastSender) Name() string { return s.name }

// Key 返回用于日志和连接复用区分的稳定键。
func (s *UDPMulticastSender) Key() string {
	return fmt.Sprintf("udp_multicast|%s->%s", s.local.String(), s.group.String())
}

// Send 选择一个 shard socket 发送当前 packet 的 payload。
func (s *UDPMulticastSender) Send(ctx context.Context, p *packet.Packet) error {
	idx := nextShardIndex(&s.nextIdx, s.shardMask)
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

// Close 关闭当前 sender 持有的全部 shard socket。
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

// getConn 获取指定 shard 的 socket，不存在时按需重建。
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

// ensureConn 在持锁后确保指定 shard 的 socket 已建立。
func (s *UDPMulticastSender) ensureConn(idx int) error {
	s.locks[idx].Lock()
	defer s.locks[idx].Unlock()
	return s.ensureConnLocked(idx)
}

// ensureConnLocked 在调用方已持有 shard 锁的前提下建立 socket。
func (s *UDPMulticastSender) ensureConnLocked(idx int) error {
	if s.conns[idx].Load() != nil {
		return nil
	}
	c, err := dialUDPWithReuse(context.Background(), s.local, s.group)
	if err != nil {
		return err
	}
	_ = c.SetWriteBuffer(s.socketSendBuffer)

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
