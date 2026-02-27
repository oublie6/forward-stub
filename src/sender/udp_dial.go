package sender

import (
	"context"
	"net"
	"syscall"

	"github.com/panjf2000/gnet/v2/pkg/socket"
)

func dialUDPWithReuse(ctx context.Context, local, remote *net.UDPAddr) (*net.UDPConn, error) {
	d := net.Dialer{
		LocalAddr: local,
		Control: func(network, address string, c syscall.RawConn) error {
			var ctrlErr error
			if err := c.Control(func(fd uintptr) {
				if err := socket.SetReuseAddr(int(fd), 1); err != nil {
					ctrlErr = err
				}
			}); err != nil {
				return err
			}
			return ctrlErr
		},
	}

	conn, err := d.DialContext(ctx, "udp", remote.String())
	if err != nil {
		return nil, err
	}
	return conn.(*net.UDPConn), nil
}
