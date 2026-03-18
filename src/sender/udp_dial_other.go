//go:build !windows && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !aix && !solaris

package sender

import "syscall"

// setSocketReuse is a package-local helper used by udp_dial_other.go.
func setSocketReuse(_ syscall.RawConn) error {
	return nil
}
