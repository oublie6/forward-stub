//go:build !windows && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !aix && !solaris

package sender

import "syscall"

// setSocketReuse 是供 udp_dial_other.go 使用的包内辅助函数。
func setSocketReuse(_ syscall.RawConn) error {
	return nil
}
