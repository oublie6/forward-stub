//go:build windows

package sender

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// setSocketReuse 是供 udp_dial_windows.go 使用的包内辅助函数。
func setSocketReuse(c syscall.RawConn) error {
	var ctrlErr error
	if err := c.Control(func(fd uintptr) {
		if err := windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_REUSEADDR, 1); err != nil {
			ctrlErr = err
		}
	}); err != nil {
		return err
	}
	return ctrlErr
}
