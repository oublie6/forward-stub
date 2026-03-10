// gnet_tcp.go 实现基于 gnet 的 TCP 发送端。
package sender

import (
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"

	"forward-stub/src/logx"
	"forward-stub/src/packet"

	"github.com/panjf2000/gnet/v2"
	"github.com/panjf2000/gnet/v2/pkg/logging"
	"github.com/valyala/bytebufferpool"
)

type GnetTCPSender struct {
	name         string
	remote       string
	withU16BELen bool
	concurrency  int
	gnetLogLevel logging.Level

	cliMu sync.Mutex
	cli   *gnet.Client

	conns atomic.Value // []gnet.Conn
	rr    uint64

	framePool bytebufferpool.Pool
}

// NewGnetTCPSender 负责该函数对应的核心逻辑，详见实现细节。
func NewGnetTCPSender(name, remote string, withU16BELen bool, concurrency int, gnetLogLevel string) (*GnetTCPSender, error) {
	if concurrency <= 0 {
		concurrency = 1
	}
	s := &GnetTCPSender{
		name:         name,
		remote:       remote,
		withU16BELen: withU16BELen,
		concurrency:  concurrency,
		gnetLogLevel: logx.ParseGnetLogLevel(gnetLogLevel),
	}
	s.conns.Store([]gnet.Conn(nil))
	if err := s.ensureClientAndDial(); err != nil {
		return nil, err
	}
	return s, nil
}

// Name 负责该函数对应的核心逻辑，详见实现细节。
func (s *GnetTCPSender) Name() string { return s.name }

// Key 负责该函数对应的核心逻辑，详见实现细节。
func (s *GnetTCPSender) Key() string { return "tcp_gnet|" + s.remote }

// Send 负责该函数对应的核心逻辑，详见实现细节。
func (s *GnetTCPSender) Send(ctx context.Context, p *packet.Packet) error {
	c := s.pickConn()
	if c == nil {
		_ = s.ensureClientAndDial()
		c = s.pickConn()
		if c == nil {
			return errors.New("no tcp conn")
		}
	}

	if !s.withU16BELen {
		if err := c.AsyncWrite(p.Payload, nil); err != nil {
			_ = c.Close()
			return err
		}
		return nil
	}

	n := len(p.Payload)
	if n > 65535 {
		return nil
	}
	bb := s.framePool.Get()
	buf := bb.B
	if cap(buf) < 2+n {
		buf = make([]byte, 2+n)
	} else {
		buf = buf[:2+n]
	}
	binary.BigEndian.PutUint16(buf[:2], uint16(n))
	copy(buf[2:], p.Payload)
	bb.B = buf

	if err := c.AsyncWrite(buf, func(gnet.Conn, error) error {
		s.releaseFrameBuf(bb)
		return nil
	}); err != nil {
		s.releaseFrameBuf(bb)
		_ = c.Close()
		return err
	}
	return nil
}

// Close 负责该函数对应的核心逻辑，详见实现细节。
func (s *GnetTCPSender) Close(ctx context.Context) error {
	if conns := s.loadConns(); len(conns) > 0 {
		for _, c := range conns {
			_ = c.Close()
		}
	}
	s.conns.Store([]gnet.Conn(nil))

	s.cliMu.Lock()
	if s.cli != nil {
		_ = s.cli.Stop()
		s.cli = nil
	}
	s.cliMu.Unlock()
	return nil
}

func (s *GnetTCPSender) loadConns() []gnet.Conn {
	v := s.conns.Load()
	if v == nil {
		return nil
	}
	conns, _ := v.([]gnet.Conn)
	return conns
}

// ensureClientAndDial 负责该函数对应的核心逻辑，详见实现细节。
func (s *GnetTCPSender) ensureClientAndDial() error {
	s.cliMu.Lock()
	defer s.cliMu.Unlock()

	if s.cli == nil {
		cli, err := gnet.NewClient(
			&clientEH{},
			gnet.WithMulticore(true),
			gnet.WithReusePort(true),
			gnet.WithReuseAddr(true),
			gnet.WithLogLevel(s.gnetLogLevel),
		)
		if err != nil {
			return err
		}
		if err := cli.Start(); err != nil {
			return err
		}
		s.cli = cli
	}

	newConns := make([]gnet.Conn, 0, s.concurrency)
	for i := 0; i < s.concurrency; i++ {
		c, err := s.cli.Dial("tcp", s.remote)
		if err != nil {
			continue
		}
		newConns = append(newConns, c)
	}

	s.conns.Store(newConns)

	if len(newConns) == 0 {
		return errors.New("tcp dial failed")
	}
	return nil
}

// pickConn 负责该函数对应的核心逻辑，详见实现细节。
func (s *GnetTCPSender) pickConn() gnet.Conn {
	conns := s.loadConns()
	if len(conns) == 0 {
		return nil
	}
	if len(conns) == 1 {
		return conns[0]
	}
	i := int(atomic.AddUint64(&s.rr, 1)-1) % len(conns)
	return conns[i]
}

func (s *GnetTCPSender) releaseFrameBuf(bb *bytebufferpool.ByteBuffer) {
	if bb == nil {
		return
	}
	b := bb.B
	if cap(b) > 64<<10 {
		bb.B = make([]byte, 0, 2048)
	} else {
		bb.B = b[:0]
	}
	s.framePool.Put(bb)
}
