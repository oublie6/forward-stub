//go:build windows

package bootstrap

import (
	"os"
	"syscall"
)

// supportedSignals is a package-local helper used by signal_windows.go.
func supportedSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}

// isReloadSignal is a package-local helper used by signal_windows.go.
func isReloadSignal(_ os.Signal) bool {
	return false
}

// isStopSignal is a package-local helper used by signal_windows.go.
func isStopSignal(sig os.Signal) bool {
	return sig == os.Interrupt || sig == syscall.SIGTERM
}
