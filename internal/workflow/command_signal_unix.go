//go:build !windows

package workflow

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func commandStageSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}

// exitedFromInterrupt reports whether the child described by err terminated
// because it received SIGINT or SIGTERM — e.g. the tty delivered Ctrl+C to the
// shared foreground group and the child died before brr's own handler observed
// anything. This lets a fast-dying child be classified as an interrupt rather
// than a stage failure.
func exitedFromInterrupt(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	ws, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok {
		return false
	}
	return ws.Signaled() && (ws.Signal() == syscall.SIGINT || ws.Signal() == syscall.SIGTERM)
}
