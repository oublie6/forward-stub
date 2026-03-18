//go:build windows

package sender

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// setSocketReuse is a package-local helper used by udp_dial_windows.go.
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
