//go:build windows

package bootstrap

import (
	"os"
	"syscall"
)

// supportedSignals 是供 signal_windows.go 使用的包内辅助函数。
func supportedSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}

// isReloadSignal 是供 signal_windows.go 使用的包内辅助函数。
func isReloadSignal(_ os.Signal) bool {
	return false
}

// isStopSignal 是供 signal_windows.go 使用的包内辅助函数。
func isStopSignal(sig os.Signal) bool {
	return sig == os.Interrupt || sig == syscall.SIGTERM
}
