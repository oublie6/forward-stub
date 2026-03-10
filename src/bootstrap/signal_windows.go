//go:build windows

package bootstrap

import (
	"os"
	"syscall"
)

func supportedSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}

func isReloadSignal(_ os.Signal) bool {
	return false
}

func isStopSignal(sig os.Signal) bool {
	return sig == os.Interrupt || sig == syscall.SIGTERM
}
