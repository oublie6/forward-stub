package sender

import (
	"context"
	"net"
	"syscall"
)

func dialUDPWithReuse(ctx context.Context, local, remote *net.UDPAddr) (*net.UDPConn, error) {
	d := net.Dialer{
		LocalAddr: local,
		Control: func(network, address string, c syscall.RawConn) error {
			return setSocketReuse(c)
		},
	}

	conn, err := d.DialContext(ctx, "udp", remote.String())
	if err != nil {
		return nil, err
	}
	return conn.(*net.UDPConn), nil
}
