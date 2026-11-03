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
}

func newTerminalGuard(command *exec.Cmd) *terminalGuard {
	guard := &terminalGuard{command: command, sessions: make(map[int]struct{})}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = guard.stop

	return guard
}

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

func (guard *terminalGuard) capture() error {
	guard.mu.Lock()
	defer guard.mu.Unlock()

	if !guard.identified {
		return nil
	}

	processes, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return fmt.Errorf("observe owned terminal sessions: %w", err)
	}

	owned := guard.descendants(processes)
	for pid := range owned {
		session, queryErr := unix.Getsid(int(pid))
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

func (guard *terminalGuard) groups() (map[int]struct{}, error) {
	guard.mu.Lock()
	defer guard.mu.Unlock()

	processes, err := unix.SysctlKinfoProcSlice("kern.proc.all")
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

		session, queryErr := unix.Getsid(pid)
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

func (guard *terminalGuard) stop() error {
	guard.once.Do(func() { guard.stopErr = guard.terminate() })

	return guard.stopErr
}

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

	err := guard.capture()
	if err != nil {
		return err
	}

	err = guard.signal(unix.SIGTERM)
	if err != nil {
		return err
	}

	time.Sleep(terminationGrace)

	err = guard.signal(unix.SIGKILL)
	if err != nil {
		return err
	}

	return guard.waitStopped()
}

func (guard *terminalGuard) waitStopped() error {
	deadline := time.Now().Add(terminationBound)

	for {
		groups, err := guard.groups()
		if err != nil {
			return err
		}

		if len(groups) == 0 {
			return nil
		}

		if time.Now().After(deadline) {
			return errTerminalStillAlive
		}

		time.Sleep(terminationPoll)
	}
}
