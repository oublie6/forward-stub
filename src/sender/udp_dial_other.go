//go:build !windows && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !aix && !solaris

package sender

import "syscall"

func setSocketReuse(_ syscall.RawConn) error {
	return nil
}
