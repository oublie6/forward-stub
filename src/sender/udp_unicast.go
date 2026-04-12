// udp_unicast_gnet.go 实现 UDP 单播发送端。
package sender

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"forward-stub/src/packet"
)

// UDPUnicastSender：方向A（最通用）实现
// - 仅使用 net.DialUDP 绑定固定源端口（local_ip:local_port）并发送到 remote
// - 设置 SO_REUSEADDR以允许多 sender 复用同一 local port
// - 保证“同一个 sender = 同一个 socket”，天然满足“单 socket 内保序”
type UDPUnicastSender struct {
	name             string
	remote           *net.UDPAddr
	local            *net.UDPAddr
	socketSendBuffer int

	concurrency int
	shardMask   int
	locks       []sync.Mutex
	conns       []atomic.Pointer[net.UDPConn]
	nextIdx     atomic.Uint64
}

// NewUDPUnicastSender 创建固定本地源端口的 UDP 单播 sender。
// 构造阶段会提前打开所有分片 socket，避免首包发送时才暴露端口占用问题。
func NewUDPUnicastSender(name, localIP string, localPort int, remote string, socketSendBuffer, concurrency int) (*UDPUnicastSender, error) {
	raddr, err := net.ResolveUDPAddr("udp", remote)
	if err != nil {
		return nil, err
	}
	if localIP == "" {
		localIP = "0.0.0.0"
	}
	lip := net.ParseIP(localIP)
	if lip == nil {
		return nil, fmt.Errorf("invalid local ip: %s", localIP)
	}
	laddr := &net.UDPAddr{IP: lip, Port: localPort}
	if concurrency <= 0 {
		concurrency = 1
	}

	s := &UDPUnicastSender{
		name:             name,
		remote:           raddr,
		local:            laddr,
		socketSendBuffer: socketSendBuffer,
		concurrency:      concurrency,
		shardMask:        concurrency - 1,
		locks:            make([]sync.Mutex, concurrency),
		conns:            make([]atomic.Pointer[net.UDPConn], concurrency),
	}
	for i := 0; i < concurrency; i++ {
		if err := s.ensureConn(i); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Name 返回 sender 配置名。
func (s *UDPUnicastSender) Name() string { return s.name }

// Key 返回本地源地址到远端地址的身份键。
func (s *UDPUnicastSender) Key() string {
	return fmt.Sprintf("udp_unicast|%s->%s", s.local.String(), s.remote.String())
}

// Send 按分片轮询 socket 写出 UDP payload。
// 正常路径只做一次原子下标递增、一次 atomic load 和一次 Write。
func (s *UDPUnicastSender) Send(ctx context.Context, p *packet.Packet) error {
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

// Close 关闭所有分片 socket；允许重复调用。
func (s *UDPUnicastSender) Close(ctx context.Context) error {
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

// getConn 在分片 socket 丢失时串行重建该分片。
func (s *UDPUnicastSender) getConn(idx int) (*net.UDPConn, error) {
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

// ensureConn 是 ensureConnLocked 的加锁包装，仅用于构造和异常重连路径。
func (s *UDPUnicastSender) ensureConn(idx int) error {
	s.locks[idx].Lock()
	defer s.locks[idx].Unlock()
	return s.ensureConnLocked(idx)
}

// ensureConnLocked 在调用者持有分片锁时创建 UDP socket。
func (s *UDPUnicastSender) ensureConnLocked(idx int) error {
	if s.conns[idx].Load() != nil {
		return nil
	}
	c, err := dialUDPWithReuse(context.Background(), s.local, s.remote)
	if err != nil {
		return err
	}
	_ = c.SetWriteBuffer(s.socketSendBuffer)
	s.conns[idx].Store(c)
	return nil
}
