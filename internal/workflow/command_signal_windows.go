//go:build windows

package workflow

import "os"

func commandStageSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}

// exitedFromInterrupt is a no-op on Windows: there is no POSIX wait-status
// signal to inspect. Ctrl+C classification relies on the recorded interrupt
// flag and the drained signal channel instead.
func exitedFromInterrupt(_ error) bool {
	return false
}
