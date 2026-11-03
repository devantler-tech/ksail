// Package caskcompletion verifies an actual interactive zsh Tab on disposable macOS runners.
package caskcompletion

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ErrTabDidNotComplete rejects an observed but uncompleted command prefix.
var ErrTabDidNotComplete = errors.New("tab did not complete the installed CLI subcommand")

// ErrNoObservation refuses success without the nonce-bound event from the actual widget.
var ErrNoObservation = errors.New("native zsh observation was not received")

// ErrUnsupportedHost keeps this actual install trial on native macOS.
var ErrUnsupportedHost = errors.New("native zsh trial requires macOS")

// Result contains only the fixed command buffer observed through ZLE.
type Result struct {
	ObservedBuffer string `json:"observedBuffer"`
	TabObserved    bool   `json:"tabObserved"`
}

const (
	trialTimeout      = 30 * time.Second
	terminalWaitDelay = 2 * time.Second
	nonceSize         = 16
	profileMode       = 0o600
	eventCapacity     = 2
	initialScanSize   = 4096
	maximumScanSize   = 64 * 1024
)

// Verify performs one bounded trial with the installed command. It never installs a cask.
func Verify(ctx context.Context) (Result, error) {
	if runtime.GOOS != "darwin" {
		return Result{}, ErrUnsupportedHost
	}

	brew, err := exec.LookPath("brew")
	if err != nil {
		return Result{}, fmt.Errorf("find Homebrew: %w", err)
	}

	return probe(ctx, brew, "ksail cl\t", "")
}

func probe(ctx context.Context, brew, input, root string) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, trialTimeout)
	defer cancel()

	work, err := os.MkdirTemp(root, "ksail-zsh-tab-")
	if err != nil {
		return Result{}, fmt.Errorf("create temporary zsh profile: %w", err)
	}

	result, probeErr := probeInWork(ctx, cancel, brew, input, work)

	return result, errors.Join(probeErr, os.RemoveAll(work))
}

func probeInWork(
	ctx context.Context,
	cancel context.CancelFunc,
	brew, input, work string,
) (Result, error) {
	nonceBytes := make([]byte, nonceSize)

	_, err := rand.Read(nonceBytes)
	if err != nil {
		return Result{}, fmt.Errorf("create observation nonce: %w", err)
	}

	nonce := hex.EncodeToString(nonceBytes)

	err = os.WriteFile(filepath.Join(work, ".zshrc"), []byte(trialProfile), profileMode)
	if err != nil {
		return Result{}, fmt.Errorf("write temporary zsh profile: %w", err)
	}

	command := exec.CommandContext(ctx, "/usr/bin/script", "-q", "/dev/null", "/bin/zsh", "-i")
	command.Dir = work // An empty directory cannot supply a filesystem completion named cluster.
	command.Env = append(os.Environ(), "ZDOTDIR="+work, "TERM=xterm-256color",
		"KSAIL_BREW_BINARY="+brew, "KSAIL_TAB_NONCE="+nonce)
	command.WaitDelay = terminalWaitDelay

	return runTerminal(ctx, cancel, command, nonce, input)
}

type observation struct {
	phase, buffer string
	err           error
}

func runTerminal(ctx context.Context, cancel context.CancelFunc, command *exec.Cmd,
	nonce, input string,
) (Result, error) {
	guard := newTerminalGuard(command)
	result, err := observeTerminal(ctx, cancel, command, guard, nonce, input)

	return result, errors.Join(err, guard.stop())
}

func observeTerminal(ctx context.Context, cancel context.CancelFunc, command *exec.Cmd,
	guard *terminalGuard, nonce, input string,
) (Result, error) {
	output, err := command.StdoutPipe()
	if err != nil {
		return Result{}, fmt.Errorf("open terminal output: %w", err)
	}

	command.Stderr = command.Stdout

	inputPipe, err := command.StdinPipe()
	if err != nil {
		return Result{}, fmt.Errorf("open terminal input: %w", err)
	}

	err = command.Start()
	if err != nil {
		return Result{}, fmt.Errorf("start native terminal: %w", err)
	}

	waited := false

	defer func() {
		cancel()

		_ = inputPipe.Close()

		if !waited {
			_ = command.Wait()
		}
	}()

	err = guard.identify()
	if err != nil {
		return Result{}, err
	}

	events := make(chan observation, eventCapacity)
	go scanEvents(ctx, output, nonce, events)

	buffer, err := observeBuffer(ctx, events, inputPipe, input, guard.capture)
	if err != nil {
		return Result{}, err
	}

	_, err = io.WriteString(inputPipe, "\x15exit\r")
	if err != nil {
		return Result{}, fmt.Errorf("exit native terminal: %w", err)
	}

	_ = inputPipe.Close()
	err = command.Wait()
	waited = true

	if err != nil {
		return Result{}, fmt.Errorf("native terminal failed: %w", err)
	}

	return completedBuffer(buffer)
}

func completedBuffer(buffer string) (Result, error) {
	result := Result{ObservedBuffer: buffer}
	if strings.TrimSpace(buffer) != "ksail cluster" {
		return result, ErrTabDidNotComplete
	}

	result.TabObserved = true

	return result, nil
}

func observeBuffer(
	ctx context.Context,
	events <-chan observation,
	inputPipe io.Writer,
	input string,
	capture func() error,
) (string, error) {
	_, err := awaitEvent(ctx, events, "READY")
	if err != nil {
		return "", err
	}

	err = capture()
	if err != nil {
		return "", err
	}

	_, err = io.WriteString(inputPipe, input+"\x18\x02")
	if err != nil {
		return "", fmt.Errorf("type Tab into native terminal: %w", err)
	}

	return awaitEvent(ctx, events, "BUFFER")
}

func scanEvents(ctx context.Context, output io.Reader, nonce string, events chan<- observation) {
	defer close(events)

	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, initialScanSize), maximumScanSize)

	for scanner.Scan() {
		phase, buffer, matched := decodeLine(scanner.Text(), nonce)
		if !matched {
			continue
		}

		select {
		case events <- observation{phase: phase, buffer: buffer}:
		case <-ctx.Done():
			return
		}
	}

	if scanner.Err() != nil {
		select {
		case events <- observation{err: ErrNoObservation}:
		case <-ctx.Done():
		}
	}
}

func awaitEvent(ctx context.Context, events <-chan observation, phase string) (string, error) {
	for {
		select {
		case event, open := <-events:
			if !open {
				return "", ErrNoObservation
			}

			if event.err != nil {
				return "", event.err
			}

			if event.phase == phase {
				return event.buffer, nil
			}
		case <-ctx.Done():
			return "", fmt.Errorf("%w: %w", ErrNoObservation, ctx.Err())
		}
	}
}

func decodeLine(line, nonce string) (string, string, bool) {
	line = strings.TrimRight(line, "\r")
	if line == nonce+":READY" {
		return "READY", "", true
	}

	prefix := nonce + ":BUFFER:"
	if after, ok := strings.CutPrefix(line, prefix); ok {
		return "BUFFER", after, true
	}

	return "", "", false
}

// The first commands are normal Homebrew zsh setup. The widget observes
// BUFFER; it neither registers a ksail completion nor inserts expected text.
const trialProfile = `__ksail_trial_shellenv="$("$KSAIL_BREW_BINARY" shellenv zsh)" || return 1
eval "$__ksail_trial_shellenv" || return 1
autoload -Uz compinit
# Use the standard completion search, without sourcing a KSail file directly.
compinit || return 1
PS1=''
__ksail_trial_buffer() {
  zle -I
  printf '\n%s:BUFFER:%s\n' "$KSAIL_TAB_NONCE" "$BUFFER"
  BUFFER=''
  zle reset-prompt
}
zle -N __ksail_trial_buffer
bindkey '^X^B' __ksail_trial_buffer
printf '\n%s:READY\n' "$KSAIL_TAB_NONCE"
`
