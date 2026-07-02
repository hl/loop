package engine

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/hl/brr/internal/fsutil"
	"github.com/hl/brr/internal/ui"
)

// gitTreeSnapshot returns an opaque fingerprint of the git working tree state
// (the `git status --porcelain` output). Callers only compare successive
// snapshots for equality to detect whether the tree changed. The second return
// value is false if git is not available or the directory is not a repository,
// in which case progress cannot be detected and failures are counted normally.
// It is a package-level var so tests can stub the tree state.
var gitTreeSnapshot = func() (string, bool) {
	out, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

const maxApprovalFileSize = 4096

const maxFailStreak = 3

// Signal file paths used by the brr engine.
const (
	SignalComplete      = ".brr-complete"
	SignalFailed        = ".brr-failed"
	SignalNeedsApproval = ".brr-needs-approval"
	SignalCycle         = ".brr-cycle"
)

// ErrInterrupted is returned when the engine is stopped by a user signal (Ctrl+C).
var ErrInterrupted = errors.New("interrupted")

// StopReason indicates why the engine stopped.
type StopReason int

const (
	ReasonComplete      StopReason = iota // .brr-complete signal file
	ReasonFailed                          // .brr-failed signal file
	ReasonApproval                        // .brr-needs-approval signal file
	ReasonCycle                           // .brr-cycle signal file
	ReasonMaxIterations                   // max iteration count reached
	ReasonFailStreak                      // too many consecutive failures
	ReasonInterrupted                     // user signal (Ctrl+C / SIGTERM)
	ReasonCommandFailed                   // a workflow command stage exited non-zero (single gate failure)
)

// Result carries the structured stop reason from a completed engine run.
type Result struct {
	Reason          StopReason
	ApprovalContent string // populated only for ReasonApproval
	FailedContent   string // populated only for ReasonFailed
}

// Options configures a loop run.
type Options struct {
	Prompt   string   // resolved prompt text
	Max      int      // max iterations (0 = unlimited)
	Command  []string // command + args to run (prompt piped to stdin)
	SkipLock bool     // skip lock acquisition (caller holds the lock)
}

// Run executes the loop until completion, max iterations, or interrupt.
// The returned Result is always non-nil when error is nil, and may also be
// non-nil on error paths to communicate the stop reason.
func Run(opts Options) (*Result, error) {
	if len(opts.Command) == 0 {
		return nil, fmt.Errorf("no command configured — set 'command' in .brr.yaml")
	}

	// Prevent concurrent brr runs in the same directory
	if !opts.SkipLock {
		lf, err := AcquireLock()
		if err != nil {
			return nil, err
		}
		defer ReleaseLock(lf)
	}

	// If signal files exist from a previous run, respect them immediately
	if sig := checkSignalFiles(); sig != nil {
		// Clean up the signal files so they don't block subsequent runs
		removeIfRegular(SignalComplete)
		removeIfRegular(SignalFailed)
		removeIfRegular(SignalNeedsApproval)
		removeIfRegular(SignalCycle)
		return &Result{Reason: sig.reason, ApprovalContent: sig.approvalContent, FailedContent: sig.failedContent}, nil
	}

	// Clean up stale signal files from previous runs
	removeIfRegular(SignalComplete)
	removeIfRegular(SignalFailed)
	removeIfRegular(SignalNeedsApproval)
	removeIfRegular(SignalCycle)

	// Clean up signal files on exit (only regular files — never delete dirs/symlinks)
	defer func() { removeIfRegular(SignalComplete) }()
	defer func() { removeIfRegular(SignalFailed) }()
	defer func() { removeIfRegular(SignalNeedsApproval) }()
	defer func() { removeIfRegular(SignalCycle) }()

	// Track the currently running subprocess so we can forward signals.
	// pendingSigINT/pendingSigTERM record signals that arrive while currentCmd
	// is nil (the window between cmd.Start() and publication); they are all
	// guarded by mu so the publish and the record decision are serialized.
	var mu sync.Mutex
	var currentCmd *exec.Cmd
	var pendingSigINT int // highest SIGINT escalation level seen with no child (2=INT, 3=KILL)
	var pendingSigTERM bool

	// Signal handling: three levels
	// 1st Ctrl+C: finish current iteration, then stop
	// 2nd Ctrl+C: send SIGINT to child (graceful shutdown)
	// 3rd Ctrl+C: force kill child
	var stopping atomic.Bool
	var sigCount atomic.Int32
	sigCh := make(chan os.Signal, 3)
	done := make(chan struct{})
	notifySignals(sigCh)
	defer signal.Stop(sigCh)

	go func() {
		for {
			select {
			case <-done:
				return
			case sig, ok := <-sigCh:
				if !ok {
					return
				}
				// SIGTERM: forward to child immediately for graceful shutdown
				if sig == sigTERM {
					stopping.Store(true)
					mu.Lock()
					cmd := currentCmd
					if cmd == nil || cmd.Process == nil {
						// No child yet — record so the publisher forwards it.
						pendingSigTERM = true
						mu.Unlock()
					} else {
						mu.Unlock()
						if err := killGroup(cmd, sigTERM); err != nil {
							fmt.Fprintf(os.Stderr, "warning: failed to forward SIGTERM to child: %v\n", err)
						}
					}
					fmt.Fprintf(os.Stderr, "\n  %s%s⏳ SIGTERM received, forwarding to child...%s\n",
						ui.Bold, ui.Yellow, ui.Reset)
					continue
				}
				// SIGINT (Ctrl+C): three escalation levels
				n := sigCount.Add(1)
				switch n {
				case 1:
					stopping.Store(true)
					fmt.Fprintf(os.Stderr, "\n  %s%s⏳ Finishing current iteration...%s (Ctrl+C again to interrupt now)\n",
						ui.Bold, ui.Yellow, ui.Reset)
				case 2:
					mu.Lock()
					cmd := currentCmd
					if cmd == nil || cmd.Process == nil {
						if pendingSigINT < 2 {
							pendingSigINT = 2
						}
						mu.Unlock()
					} else {
						mu.Unlock()
						if err := killGroup(cmd, sigINT); err != nil {
							fmt.Fprintf(os.Stderr, "warning: failed to interrupt child: %v\n", err)
						}
					}
				default:
					mu.Lock()
					cmd := currentCmd
					if cmd == nil || cmd.Process == nil {
						pendingSigINT = 3
						mu.Unlock()
					} else {
						mu.Unlock()
						if err := killGroup(cmd, sigKILL); err != nil {
							fmt.Fprintf(os.Stderr, "warning: failed to force-kill child: %v\n", err)
						}
					}
				}
			}
		}
	}()
	defer close(done)

	failStreak := 0
	var lastErr error
	i := 0

	for opts.Max == 0 || i < opts.Max {
		// Check if user requested stop (first Ctrl+C) between iterations
		if stopping.Load() {
			fmt.Fprintf(os.Stderr, "\n  %s%sStopped%s.\n", ui.Bold, ui.Yellow, ui.Reset)
			return &Result{Reason: ReasonInterrupted}, ErrInterrupted
		}

		if sig := checkSignalFiles(); sig != nil {
			return &Result{Reason: sig.reason, ApprovalContent: sig.approvalContent, FailedContent: sig.failedContent}, nil
		}

		// Print iteration header
		iterNum := i + 1
		maxLabel := ""
		if opts.Max > 0 {
			maxLabel = fmt.Sprintf("/%d", opts.Max)
		}
		fmt.Fprintf(os.Stderr, "\n%s━━━%s %s%sIteration %d%s%s %s▸ %s ━━━%s\n",
			ui.Dim, ui.Reset,
			ui.Bold, ui.Cyan, iterNum, maxLabel, ui.Reset,
			ui.Dim, time.Now().Format("15:04:05"), ui.Reset,
		)

		// Snapshot the working tree before the iteration so we can tell whether
		// the agent made progress (changed the tree) if the command later fails.
		treeBefore, treeTracked := gitTreeSnapshot()

		// Run the command with prompt piped to stdin.
		cmd := exec.Command(opts.Command[0], opts.Command[1:]...)
		cmd.Stdin = strings.NewReader(opts.Prompt)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		setProcAttr(cmd)

		// Start then publish: ensures cmd.Process is populated before the signal
		// handler can see currentCmd, preventing races where signals arrive between
		// setting currentCmd and the process actually existing.
		if err := cmd.Start(); err != nil {
			mu.Lock()
			currentCmd = nil
			mu.Unlock()
			// Start failure counts as iteration failure
			failStreak++
			lastErr = err
			fmt.Fprintf(os.Stderr, "  %s%s✗ Iteration %d failed to start%s: %v. Consecutive failures: %d/%d\n",
				ui.Bold, ui.Red, iterNum, ui.Reset, err, failStreak, maxFailStreak,
			)
			if failStreak >= maxFailStreak {
				fmt.Fprintf(os.Stderr, "  %s%s✗ Too many consecutive failures. Stopping.%s\n", ui.Bold, ui.Red, ui.Reset)
				return &Result{Reason: ReasonFailStreak}, fmt.Errorf("stopped after %d consecutive failures: %w", maxFailStreak, lastErr)
			}
			i++
			continue
		}

		mu.Lock()
		currentCmd = cmd
		// Reconcile signals that arrived during the Start→publish window: the
		// handler saw currentCmd == nil and recorded them instead of forwarding.
		// Reading and clearing under the same lock as the publish guarantees
		// exactly-once delivery with the handler (no drop, no double-send).
		pInt := pendingSigINT
		pTerm := pendingSigTERM
		pendingSigINT = 0
		pendingSigTERM = false
		mu.Unlock()
		for _, sig := range pendingSignalsToForward(pInt, pTerm) {
			if err := killGroup(cmd, sig); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to forward pending signal to child: %v\n", err)
			}
		}

		err := cmd.Wait()

		mu.Lock()
		currentCmd = nil
		mu.Unlock()

		// Clean up orphaned child processes (MCP servers, language servers, etc.)
		// that outlive the agent process and would otherwise accumulate across iterations.
		reapGroup(cmd)

		// Check for signal files immediately after subprocess exits
		if sig := checkSignalFiles(); sig != nil {
			return &Result{Reason: sig.reason, ApprovalContent: sig.approvalContent, FailedContent: sig.failedContent}, nil
		}

		// If user requested stop (first Ctrl+C), exit gracefully now that the iteration is done
		if stopping.Load() {
			fmt.Fprintf(os.Stderr, "\n  %s%sStopped after iteration %d%s.\n", ui.Bold, ui.Yellow, iterNum, ui.Reset)
			return &Result{Reason: ReasonInterrupted}, ErrInterrupted
		}

		if err != nil {
			lastErr = err
			rc := 1
			if exitErr, ok := err.(*exec.ExitError); ok {
				rc = exitErr.ExitCode()
			}
			// If the working tree *changed* during this iteration, the agent made
			// progress before crashing (e.g. context exhaustion, timeout). Don't
			// count it toward the fail streak — the next iteration's recovery phase
			// will pick up where it left off. A tree that is merely dirty but
			// unchanged (pre-existing edits, a deterministic no-op failure) still
			// counts, so the fail-streak breaker cannot be permanently disabled.
			treeAfter, treeTrackedAfter := gitTreeSnapshot()
			progressed := treeTracked && treeTrackedAfter && treeAfter != treeBefore
			if progressed {
				failStreak = 0
				fmt.Fprintf(os.Stderr, "  %s%s⟳ Iteration %d crashed%s (exit %d) — working tree changed, progress was made. Retrying.\n",
					ui.Bold, ui.Yellow, iterNum, ui.Reset, rc,
				)
			} else {
				failStreak++
				fmt.Fprintf(os.Stderr, "  %s%s✗ Iteration %d failed%s (exit %d). Consecutive failures: %d/%d\n",
					ui.Bold, ui.Red, iterNum, ui.Reset, rc, failStreak, maxFailStreak,
				)
				if failStreak >= maxFailStreak {
					fmt.Fprintf(os.Stderr, "  %s%s✗ Too many consecutive failures. Stopping.%s\n", ui.Bold, ui.Red, ui.Reset)
					return &Result{Reason: ReasonFailStreak}, fmt.Errorf("stopped after %d consecutive failures: %w", maxFailStreak, lastErr)
				}
			}
		} else {
			failStreak = 0
			lastErr = nil
		}

		// i counts total attempts, including failures
		i++
	}

	if lastErr != nil {
		return &Result{Reason: ReasonMaxIterations}, fmt.Errorf("last iteration failed: %w", lastErr)
	}
	return &Result{Reason: ReasonMaxIterations}, nil
}

// pendingSignalsToForward returns, in delivery order, the signals that must be
// forwarded to a freshly-published child to make up for signals that arrived
// during the Start→publish window (when the handler saw a nil child). SIGTERM,
// if seen, is delivered first; then the highest SIGINT escalation level reached
// (SIGINT at level 2, SIGKILL at level 3). This preserves the graceful-interrupt
// escalation instead of letting a dropped level-2 SIGINT skip straight to KILL.
func pendingSignalsToForward(pendingINT int, pendingTERM bool) []syscall.Signal {
	var sigs []syscall.Signal
	if pendingTERM {
		sigs = append(sigs, sigTERM)
	}
	switch {
	case pendingINT >= 3:
		sigs = append(sigs, sigKILL)
	case pendingINT == 2:
		sigs = append(sigs, sigINT)
	}
	return sigs
}

// removeIfRegular removes path only if it is a regular file.
func removeIfRegular(path string) {
	if fsutil.IsRegularFile(path) {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "warning: could not clean up %s: %v\n", path, err)
		}
	}
}

// signalResult is returned by checkSignalFiles when a signal file is found.
type signalResult struct {
	reason          StopReason
	approvalContent string
	failedContent   string
}

// checkSignalFiles checks for .brr-complete, .brr-failed, .brr-needs-approval,
// and .brr-cycle.
// Only regular files are treated as signals (symlinks and directories are ignored).
// Returns nil if no signal file was found.
func checkSignalFiles() *signalResult {
	if fsutil.IsRegularFile(SignalComplete) {
		fmt.Fprintf(os.Stderr, "\n  %s%s✓ All tasks complete%s (%s found). Stopping.\n", ui.Bold, ui.Green, ui.Reset, SignalComplete)
		return &signalResult{reason: ReasonComplete}
	}
	if f, err := fsutil.OpenRegularFile(SignalFailed); err == nil {
		fmt.Fprintf(os.Stderr, "\n  %s%s✗ Agent failed%s (%s found):\n", ui.Bold, ui.Red, ui.Reset, SignalFailed)
		content, readErr := readCappedFromFile(f, maxApprovalFileSize)
		_ = f.Close()
		var failedContent string
		if readErr == nil {
			trimmed := strings.TrimSpace(content)
			if trimmed != "" {
				fmt.Fprintln(os.Stderr, trimmed)
				failedContent = trimmed
			} else {
				fmt.Fprintln(os.Stderr, "  (no details provided)")
			}
		} else {
			fmt.Fprintf(os.Stderr, "  (could not read details: %v)\n", readErr)
		}
		return &signalResult{reason: ReasonFailed, failedContent: failedContent}
	} else if fsutil.IsRegularFile(SignalFailed) {
		fmt.Fprintf(os.Stderr, "\n  %s%s✗ Agent failed%s (%s found):\n", ui.Bold, ui.Red, ui.Reset, SignalFailed)
		fmt.Fprintf(os.Stderr, "  (could not read details: %v)\n", err)
		return &signalResult{reason: ReasonFailed}
	}
	// Try to open and read in one pass; fall back to existence check for unreadable files
	if f, err := fsutil.OpenRegularFile(SignalNeedsApproval); err == nil {
		fmt.Fprintf(os.Stderr, "\n  %s%s⏸ Task needs human approval%s (%s found):\n", ui.Bold, ui.Yellow, ui.Reset, SignalNeedsApproval)
		content, readErr := readCappedFromFile(f, maxApprovalFileSize)
		_ = f.Close()
		var approvalContent string
		if readErr == nil {
			trimmed := strings.TrimSpace(content)
			if trimmed != "" {
				fmt.Fprintln(os.Stderr, trimmed)
				approvalContent = trimmed
			} else {
				fmt.Fprintln(os.Stderr, "  (no details provided)")
			}
		} else {
			fmt.Fprintf(os.Stderr, "  (could not read details: %v)\n", readErr)
		}
		return &signalResult{reason: ReasonApproval, approvalContent: approvalContent}
	} else if fsutil.IsRegularFile(SignalNeedsApproval) {
		// File exists but can't be opened (e.g. permissions) — still honor the signal
		fmt.Fprintf(os.Stderr, "\n  %s%s⏸ Task needs human approval%s (%s found):\n", ui.Bold, ui.Yellow, ui.Reset, SignalNeedsApproval)
		fmt.Fprintf(os.Stderr, "  (could not read details: %v)\n", err)
		return &signalResult{reason: ReasonApproval}
	}
	if fsutil.IsRegularFile(SignalCycle) {
		fmt.Fprintf(os.Stderr, "\n  %s%s↻ Cycle requested%s (%s found). Stopping this stage.\n", ui.Bold, ui.Magenta, ui.Reset, SignalCycle)
		return &signalResult{reason: ReasonCycle}
	}
	return nil
}

// readCappedFromFile reads up to maxBytes from an already-open file, appending a truncation notice if needed.
// Truncation is aligned to a valid UTF-8 boundary to avoid garbled output.
func readCappedFromFile(f *os.File, maxBytes int64) (string, error) {
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > maxBytes {
		// Walk backwards to a UTF-8 rune boundary (at most 3 bytes back)
		cut := int(maxBytes)
		for cut > 0 && !utf8.RuneStart(data[cut]) {
			cut--
		}
		return string(data[:cut]) + "\n  ... (truncated)", nil
	}
	return string(data), nil
}
