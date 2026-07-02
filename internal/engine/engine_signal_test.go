//go:build !windows

package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// waitForFile polls until path exists or timeout expires.
func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func TestSignalFirstCtrlCStopsAfterIteration(t *testing.T) {
	t.Chdir(t.TempDir())

	// Command runs briefly and creates a marker file on completion.
	marker := filepath.Join(".", "iteration-done")
	cmd := []string{"sh", "-c", fmt.Sprintf("sleep 0.3 && touch %s", marker)}

	var runErr error
	done := make(chan struct{})
	go func() {
		_, runErr = Run(Options{Prompt: "test", Max: 100, Command: cmd})
		close(done)
	}()

	// Wait for child to start, then send first Ctrl+C
	time.Sleep(100 * time.Millisecond)
	syscall.Kill(os.Getpid(), syscall.SIGINT)

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("engine did not stop within 10s after first Ctrl+C")
	}

	if !errors.Is(runErr, ErrInterrupted) {
		t.Errorf("expected ErrInterrupted, got: %v", runErr)
	}

	// First Ctrl+C should let the current iteration finish — marker should exist
	if _, err := os.Stat(marker); os.IsNotExist(err) {
		t.Error("expected current iteration to complete before stopping")
	}
}

func TestSignalSecondCtrlCInterruptsChild(t *testing.T) {
	t.Chdir(t.TempDir())

	// Command that creates a marker then sleeps for a long time
	marker := filepath.Join(".", "child-started")
	cmd := []string{"sh", "-c", fmt.Sprintf("touch %s && sleep 60", marker)}

	var runErr error
	done := make(chan struct{})
	go func() {
		_, runErr = Run(Options{Prompt: "test", Max: 100, Command: cmd})
		close(done)
	}()

	waitForFile(t, marker, 5*time.Second)

	// 1st Ctrl+C: request graceful stop
	syscall.Kill(os.Getpid(), syscall.SIGINT)
	time.Sleep(100 * time.Millisecond)

	// 2nd Ctrl+C: interrupt child (SIGINT to process group)
	syscall.Kill(os.Getpid(), syscall.SIGINT)

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("engine did not stop within 10s after 2nd Ctrl+C")
	}

	if !errors.Is(runErr, ErrInterrupted) {
		t.Errorf("expected ErrInterrupted, got: %v", runErr)
	}
}

func TestSignalThirdCtrlCForceKills(t *testing.T) {
	t.Chdir(t.TempDir())

	// Command that traps SIGINT and refuses to die gracefully
	marker := filepath.Join(".", "child-started")
	cmd := []string{"sh", "-c", fmt.Sprintf("trap '' INT; touch %s; sleep 60", marker)}

	var runErr error
	done := make(chan struct{})
	go func() {
		_, runErr = Run(Options{Prompt: "test", Max: 100, Command: cmd})
		close(done)
	}()

	waitForFile(t, marker, 5*time.Second)

	// Send all three signals with small delays
	syscall.Kill(os.Getpid(), syscall.SIGINT)
	time.Sleep(100 * time.Millisecond)
	syscall.Kill(os.Getpid(), syscall.SIGINT)
	time.Sleep(100 * time.Millisecond)
	syscall.Kill(os.Getpid(), syscall.SIGINT) // SIGKILL to child

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("engine did not stop within 10s after 3rd Ctrl+C")
	}

	// After force-kill, we still expect ErrInterrupted
	if !errors.Is(runErr, ErrInterrupted) {
		t.Errorf("expected ErrInterrupted, got: %v", runErr)
	}
}

func TestSignalInterruptBeatsSignalFile(t *testing.T) {
	t.Chdir(t.TempDir())

	// The child sleeps (giving the SIGINT time to set the stopping flag), then
	// writes .brr-complete and exits 0. The interrupt must win over the complete
	// signal: the engine stops with ReasonInterrupted, not ReasonComplete.
	cmd := []string{"sh", "-c", "sleep 0.4 && touch " + SignalComplete}

	var result *Result
	var runErr error
	done := make(chan struct{})
	go func() {
		result, runErr = Run(Options{Prompt: "test", Max: 1, Command: cmd})
		close(done)
	}()

	time.Sleep(150 * time.Millisecond)
	syscall.Kill(os.Getpid(), syscall.SIGINT)

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("engine did not stop within 10s")
	}

	if !errors.Is(runErr, ErrInterrupted) {
		t.Errorf("expected ErrInterrupted, got: %v", runErr)
	}
	if result == nil || result.Reason != ReasonInterrupted {
		t.Errorf("expected ReasonInterrupted (interrupt beats .brr-complete), got %#v", result)
	}
}

func TestSignalSIGTERMForwardsToChild(t *testing.T) {
	t.Chdir(t.TempDir())

	// Command that creates a marker then sleeps
	marker := filepath.Join(".", "child-started")
	cmd := []string{"sh", "-c", fmt.Sprintf("touch %s && sleep 60", marker)}

	var runErr error
	done := make(chan struct{})
	go func() {
		_, runErr = Run(Options{Prompt: "test", Max: 100, Command: cmd})
		close(done)
	}()

	waitForFile(t, marker, 5*time.Second)

	// SIGTERM should be forwarded to child and stop the engine
	syscall.Kill(os.Getpid(), syscall.SIGTERM)

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("engine did not stop within 10s after SIGTERM")
	}

	// SIGTERM sets stopping=true, so after iteration ends the engine should return ErrInterrupted
	if !errors.Is(runErr, ErrInterrupted) {
		t.Errorf("expected ErrInterrupted, got: %v", runErr)
	}
}
