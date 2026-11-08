package caskcompletion

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	terminationGrace = 150 * time.Millisecond
	terminationBound = time.Second
	terminationPoll  = 10 * time.Millisecond
	darwinZombie     = 5 // SZOMB in Darwin's sys/proc.h; a dead process awaiting its parent's wait.
)

var errTerminalStillAlive = errors.New("owned native terminal processes remain alive")

// script creates a separate PTY session; Setpgid alone cannot contain its shell.
// Capture descendant-created sessions while the parent is alive, before typing Tab.
type terminalGuard struct {
	command       *exec.Cmd
	mu            sync.Mutex
	started       unix.Timeval
	identified    bool
	parentSession int
	sessions      map[int]struct{}
	once          sync.Once
	stopErr       error
	processes     func() ([]unix.KinfoProc, error)
	session       func(int) (int, error)
}

// newTerminalGuard gives script its own process group and binds cancellation to owned-session cleanup.
func newTerminalGuard(command *exec.Cmd) *terminalGuard {
	guard := &terminalGuard{
		command:   command,
		sessions:  make(map[int]struct{}),
		processes: func() ([]unix.KinfoProc, error) { return unix.SysctlKinfoProcSlice("kern.proc.all") },
		session:   unix.Getsid,
	}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = guard.stop

	return guard
}

// identify pins the parent's kernel start time and caller session before observing descendants.
func (guard *terminalGuard) identify() error {
	process, err := unix.SysctlKinfoProc("kern.proc.pid", guard.command.Process.Pid)
	if err != nil {
		return fmt.Errorf("identify native terminal parent: %w", err)
	}

	session, err := unix.Getsid(guard.command.Process.Pid)
	if err != nil {
		return fmt.Errorf("identify native terminal parent session: %w", err)
	}

	guard.mu.Lock()
	defer guard.mu.Unlock()

	guard.started, guard.parentSession, guard.identified = process.Proc.P_starttime, session, true

	return nil
}

// capture admits only descendant-created sessions whose leaders are still in the verified ancestry.
// The caller's session is never admitted, even though script initially inherits it.
func (guard *terminalGuard) capture() error {
	guard.mu.Lock()
	defer guard.mu.Unlock()

	if !guard.identified {
		return nil
	}

	processes, err := guard.processes()
	if err != nil {
		return fmt.Errorf("observe owned terminal sessions: %w", err)
	}

	owned := guard.descendants(processes)
	for pid := range owned {
		session, queryErr := guard.session(int(pid))
		if errors.Is(queryErr, unix.ESRCH) {
			continue
		}

		if queryErr != nil {
			return fmt.Errorf("observe owned PTY session: %w", queryErr)
		}

		if session != guard.parentSession &&
			owned[int32(session)] { //nolint:gosec // Darwin pid_t is int32.
			guard.sessions[session] = struct{}{}
		}
	}

	return nil
}

// descendants roots ancestry in both the parent PID and start time to reject a recycled PID.
func (guard *terminalGuard) descendants(processes []unix.KinfoProc) map[int32]bool {
	owned := map[int32]bool{}

	for _, process := range processes {
		if int(process.Proc.P_pid) == guard.command.Process.Pid &&
			process.Proc.P_starttime == guard.started {
			owned[process.Proc.P_pid] = true
		}
	}

	for changed := true; changed; {
		changed = false

		for _, process := range processes {
			if owned[process.Eproc.Ppid] && !owned[process.Proc.P_pid] {
				owned[process.Proc.P_pid], changed = true, true
			}
		}
	}

	return owned
}

// groups returns live, same-user groups in admitted PTY sessions plus the verified parent group.
// Failed ownership observations remain errors; zombies cannot keep cleanup pending.
func (guard *terminalGuard) groups() (map[int]struct{}, error) {
	guard.mu.Lock()
	defer guard.mu.Unlock()

	processes, err := guard.processes()
	if err != nil {
		return nil, fmt.Errorf("read owned terminal liveness: %w", err)
	}

	groups := make(map[int]struct{})

	for _, process := range processes {
		if process.Proc.P_stat == darwinZombie {
			continue
		}

		if int64(process.Eproc.Pcred.P_ruid) != int64(unix.Getuid()) {
			continue
		}

		pid := int(process.Proc.P_pid)
		if pid == guard.command.Process.Pid && process.Proc.P_starttime == guard.started {
			groups[pid] = struct{}{}
		}

		session, queryErr := guard.session(pid)
		if errors.Is(queryErr, unix.ESRCH) {
			continue
		}

		if queryErr != nil {
			return nil, fmt.Errorf("read owned PTY liveness: %w", queryErr)
		}

		if _, owned := guard.sessions[session]; owned {
			groups[int(process.Eproc.Pgid)] = struct{}{}
		}
	}

	return groups, nil
}

// signal rechecks ownership before signaling groups, including disowned jobs within an admitted session.
func (guard *terminalGuard) signal(signal unix.Signal) error {
	groups, err := guard.groups()
	if err != nil {
		return err
	}

	for group := range groups {
		if group <= 1 {
			return errTerminalStillAlive
		}

		err = unix.Kill(-group, signal)
		if err != nil && !errors.Is(err, unix.ESRCH) {
			return fmt.Errorf("signal owned native terminal group: %w", err)
		}
	}

	return nil
}

// stop shares one termination result between CommandContext cancellation and normal-return cleanup.
func (guard *terminalGuard) stop() error {
	guard.once.Do(func() { guard.stopErr = guard.terminate() })

	return guard.stopErr
}

// terminate escalates TERM to KILL only for proven owned groups and refuses incomplete liveness proof.
// Query retries share the existing grace-plus-wait budget rather than granting each phase a new one.
func (guard *terminalGuard) terminate() error {
	if guard.command.Process == nil {
		return nil
	}

	guard.mu.Lock()
	identified := guard.identified
	guard.mu.Unlock()

	if !identified {
		err := guard.command.Process.Kill()
		if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
			return nil
		}

		if err != nil {
			return fmt.Errorf("stop unidentified native terminal parent: %w", err)
		}

		return nil
	}

	err := guard.terminateOwned()
	if err == nil {
		return nil
	}

	// Ownership was not proven within the budget, so no group was signaled. The direct parent
	// is still this process's own unreaped child: its PID cannot be reused before Wait, so
	// killing it cannot reach an unrelated process, and it no longer outlives its profile.
	killErr := guard.command.Process.Kill()
	if errors.Is(killErr, os.ErrProcessDone) || errors.Is(killErr, syscall.ESRCH) {
		killErr = nil
	}

	if killErr != nil {
		killErr = fmt.Errorf("stop native terminal parent after unproven ownership: %w", killErr)
	}

	return errors.Join(err, killErr)
}

// terminateOwned signals only groups a complete kernel observation proves this guard owns.
func (guard *terminalGuard) terminateOwned() error {
	// Capture, signaling and liveness share the former grace-plus-wait allowance.
	// A transient observation failure cannot bypass termination or extend its budget.
	deadline := time.Now().Add(terminationGrace + terminationBound)

	err := retryTerminalQuery(deadline, guard.capture)
	if err != nil {
		return err
	}

	err = retryTerminalQuery(deadline, func() error { return guard.signal(unix.SIGTERM) })
	if err != nil {
		return err
	}

	time.Sleep(min(terminationGrace, max(0, time.Until(deadline))))

	err = retryTerminalQuery(deadline, func() error { return guard.signal(unix.SIGKILL) })
	if err != nil {
		return err
	}

	return guard.waitStopped(deadline)
}

// retryTerminalQuery retains the last ownership/query error after a shared, bounded retry window.
func retryTerminalQuery(deadline time.Time, query func() error) error {
	for {
		err := query()
		if err == nil {
			return nil
		}

		if !time.Now().Before(deadline) {
			return err
		}

		time.Sleep(min(terminationPoll, time.Until(deadline)))
	}
}

// waitStopped refuses success until a complete kernel observation finds no owned live groups.
func (guard *terminalGuard) waitStopped(deadline time.Time) error {
	return retryTerminalQuery(deadline, func() error {
		groups, err := guard.groups()
		if err != nil {
			return err
		}

		if len(groups) != 0 {
			return errTerminalStillAlive
		}

		return nil
	})
}
