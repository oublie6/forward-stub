//go:build !windows

package bootstrap

import (
	"os"
	"syscall"
)

func supportedSignals() []os.Signal {
	return []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGUSR1}
}

func isReloadSignal(sig os.Signal) bool {
	return sig == syscall.SIGHUP || sig == syscall.SIGUSR1
}

func isStopSignal(sig os.Signal) bool {
	return sig == syscall.SIGINT || sig == syscall.SIGTERM
}
