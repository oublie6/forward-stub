//go:build !windows

package bootstrap

import (
	"os"
	"syscall"
)

// supportedSignals 是供 signal_unix.go 使用的包内辅助函数。
func supportedSignals() []os.Signal {
	return []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGUSR1}
}

// isReloadSignal 是供 signal_unix.go 使用的包内辅助函数。
func isReloadSignal(sig os.Signal) bool {
	return sig == syscall.SIGHUP || sig == syscall.SIGUSR1
}

// isStopSignal 是供 signal_unix.go 使用的包内辅助函数。
func isStopSignal(sig os.Signal) bool {
	return sig == syscall.SIGINT || sig == syscall.SIGTERM
}
