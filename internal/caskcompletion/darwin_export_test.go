//go:build darwin

package caskcompletion

import (
	"fmt"
	"os/exec"

	"golang.org/x/sys/unix"
)

// TerminalGuardForTest exposes per-instance kernel-query faults without changing other trials.
type TerminalGuardForTest struct{ guard *terminalGuard }

// NewTerminalGuardForTest binds the real owned process group before the caller starts it.
func NewTerminalGuardForTest(command *exec.Cmd) *TerminalGuardForTest {
	return &TerminalGuardForTest{guard: newTerminalGuard(command)}
}

// CaptureForTest identifies the actual parent and observes its live PTY descendants.
func (guard *TerminalGuardForTest) CaptureForTest() error {
	err := guard.guard.identify()
	if err != nil {
		return err
	}

	return guard.guard.capture()
}

// StopWithQueryFaultForTest faults one cleanup phase, or all queries from that phase onward.
// Each real process listing identifies a phase: capture, TERM, KILL, then liveness.
func (guard *TerminalGuardForTest) StopWithQueryFaultForTest(
	query string, phase int, persistent bool, fault error,
) error {
	list := guard.guard.processes
	session := guard.guard.session
	queries := 0
	failed := false
	shouldFail := func() bool {
		if queries < phase || (failed && !persistent) {
			return false
		}

		failed = true

		return true
	}
	guard.guard.processes = func() ([]unix.KinfoProc, error) {
		queries++

		if query == "list" && shouldFail() {
			return nil, fault
		}

		return list()
	}
	guard.guard.session = func(pid int) (int, error) {
		if query == "session" && shouldFail() {
			return 0, fault
		}

		return session(pid)
	}

	return guard.guard.stop()
}

// WaitParentForTest reaps the guarded parent so a test can read how it ended.
func (guard *TerminalGuardForTest) WaitParentForTest() error {
	err := guard.guard.command.Wait()
	if err != nil {
		return fmt.Errorf("wait for guarded parent: %w", err)
	}

	return nil
}

// KillParentForTest stops the guarded parent directly when a test must reap it itself.
func (guard *TerminalGuardForTest) KillParentForTest() error {
	err := guard.guard.command.Process.Kill()
	if err != nil {
		return fmt.Errorf("kill guarded parent: %w", err)
	}

	return nil
}
