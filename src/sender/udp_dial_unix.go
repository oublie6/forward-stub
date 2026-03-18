//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || aix || solaris

package sender

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// setSocketReuse 是供 udp_dial_unix.go 使用的包内辅助函数。
func setSocketReuse(c syscall.RawConn) error {
	var ctrlErr error
	if err := c.Control(func(fd uintptr) {
		if err := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
			ctrlErr = err
		}
	}); err != nil {
		return err
	}
	return ctrlErr
}
