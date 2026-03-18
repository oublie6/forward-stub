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

// UDPUnicastSender 负责把处理后的 packet 发送到固定 UDP 单播目标。
//
// 设计说明：
//   - sender 与 selector 解耦，只处理 task fan-out 之后的 egress；
//   - 通过 shard socket 提升并发发送能力；
//   - 每个 shard 内仍保持单 socket 写入语义。
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

// NewUDPUnicastSender 创建并预热单播 sender 需要的 socket 分片。
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

// Name 返回 sender 的配置名称。
func (s *UDPUnicastSender) Name() string { return s.name }

// Key 返回用于日志和连接复用区分的稳定键。
func (s *UDPUnicastSender) Key() string {
	return fmt.Sprintf("udp_unicast|%s->%s", s.local.String(), s.remote.String())
}

// Send 选择一个 shard socket 发送当前 packet 的 payload。
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

// Close 关闭当前 sender 持有的全部 shard socket。
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

// getConn 获取指定 shard 的 socket，不存在时按需重建。
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

// ensureConn 在持锁后确保指定 shard 的 socket 已建立。
func (s *UDPUnicastSender) ensureConn(idx int) error {
	s.locks[idx].Lock()
	defer s.locks[idx].Unlock()
	return s.ensureConnLocked(idx)
}

// ensureConnLocked 在调用方已持有 shard 锁的前提下建立 socket。
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
