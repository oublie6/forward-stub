//go:build !windows

package bootstrap

import (
	"os"
	"syscall"
)

// supportedSignals is a package-local helper used by signal_unix.go.
func supportedSignals() []os.Signal {
	return []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGUSR1}
}

// isReloadSignal is a package-local helper used by signal_unix.go.
func isReloadSignal(sig os.Signal) bool {
	return sig == syscall.SIGHUP || sig == syscall.SIGUSR1
}

// isStopSignal is a package-local helper used by signal_unix.go.
func isStopSignal(sig os.Signal) bool {
	return sig == syscall.SIGINT || sig == syscall.SIGTERM
}
