package hetznerbase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	sshbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/ssh"
	gossh "golang.org/x/crypto/ssh"
)

const (
	// DefaultSSHWaitTimeout bounds the wait for a created node's sshd to accept
	// the bootstrap dial when [Base.BringUpSSHTimeout] is zero. A stock image
	// brings sshd up within a minute or two of the server-create action.
	DefaultSSHWaitTimeout = 5 * time.Minute

	// DefaultBootstrapWaitTimeout bounds the wait for a node's first-boot
	// bootstrap (cloud-init installing and starting the distribution) to write
	// its admin kubeconfig when [Base.BringUpBootstrapTimeout] is zero.
	DefaultBootstrapWaitTimeout = 20 * time.Minute

	// bootstrapDiagnosticsTimeout bounds the one diagnostic read taken from a
	// node whose bootstrap wait timed out.
	bootstrapDiagnosticsTimeout = 30 * time.Second

	// bootstrapDiagnosticsCommand reports cloud-init's own verdict on the first
	// boot. Deliberately status only: the cloud-init output log carries the
	// distribution's install transcript, which can include join tokens.
	bootstrapDiagnosticsCommand = "cloud-init status --long"

	// bootstrapStatusCommand is the short cloud-init verdict probed between
	// polls, so a first boot that has already failed ends the wait at once.
	bootstrapStatusCommand = "cloud-init status"

	// bootstrapFailedStatus is the line bootstrapStatusCommand prints once
	// cloud-init finished with an unrecoverable error.
	bootstrapFailedStatus = "status: error"

	// bootstrapErrorLinesCommand extracts only kubeadm's own error lines from
	// the cloud-init output log (preflight "[ERROR ...]" checks and the failed
	// phase), never the rest of the transcript, which carries join tokens.
	bootstrapErrorLinesCommand = "grep -E '^(\\[ERROR |error execution phase)' " +
		"/var/log/cloud-init-output.log | tail -n 20"

	// maxBootstrapDiagnosticsBytes caps how much of the diagnostic output is
	// carried into the returned error.
	maxBootstrapDiagnosticsBytes = 2048
)

// ErrBringUpStageTimeout is returned when a bring-up stage does not finish
// within its own deadline. The wrapping error names the stage and the node.
var ErrBringUpStageTimeout = errors.New("hetzner: bring-up stage did not finish in time")

// ErrBootstrapFailed is returned when cloud-init reports a node's first boot
// as failed while the bring-up is still waiting for the bootstrap to finish.
var ErrBootstrapFailed = errors.New("hetzner: first-boot bootstrap failed")

// logf writes one progress line to the Base's LogWriter, if it has one.
func (b *Base) logf(format string, args ...any) {
	if b.LogWriter == nil {
		return
	}

	_, _ = fmt.Fprintf(b.LogWriter, format+"\n", args...)
}

// sshWaitTimeout is the configured SSH wait bound, or its default.
func (b *Base) sshWaitTimeout() time.Duration {
	if b.BringUpSSHTimeout > 0 {
		return b.BringUpSSHTimeout
	}

	return DefaultSSHWaitTimeout
}

// bootstrapWaitTimeout is the configured bootstrap wait bound, or its default.
func (b *Base) bootstrapWaitTimeout() time.Duration {
	if b.BringUpBootstrapTimeout > 0 {
		return b.BringUpBootstrapTimeout
	}

	return DefaultBootstrapWaitTimeout
}

// dialBootstrapSSH waits, within the SSH stage's own deadline, for the node at
// addr to accept the bootstrap dial.
func (b *Base) dialBootstrapSSH(
	ctx context.Context,
	addr string,
	signer gossh.Signer,
	hostKeyCallback gossh.HostKeyCallback,
) (*sshbootstrap.Client, error) {
	timeout := b.sshWaitTimeout()
	b.logf("Waiting for SSH on %s (up to %s)...", addr, timeout)

	stageCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client, err := sshbootstrap.DialWithRetry(stageCtx, sshbootstrap.Options{
		Addr:            addr,
		User:            bootstrapUser,
		Signer:          signer,
		HostKeyCallback: hostKeyCallback,
	}, 0)
	if err != nil {
		if stageTimedOut(ctx, stageCtx) {
			return nil, fmt.Errorf(
				"%w: waiting for SSH on %s after %s: %w",
				ErrBringUpStageTimeout, addr, timeout, err,
			)
		}

		return nil, fmt.Errorf("dial bootstrap SSH at %s: %w", addr, err)
	}

	b.logf("✓ SSH is up on %s", addr)

	return client, nil
}

// waitForBootstrapFile waits, within the bootstrap stage's own deadline, for
// the node's first boot to write path. On a timeout it attaches the node's
// cloud-init status so the error says why the bootstrap did not finish.
func (b *Base) waitForBootstrapFile(
	ctx context.Context,
	client *sshbootstrap.Client,
	addr string,
	path string,
	interval time.Duration,
) error {
	timeout := b.bootstrapWaitTimeout()
	b.logf("Waiting for the first-boot bootstrap to write %s on %s (up to %s)...",
		path, addr, timeout)

	stageCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	err := waitForRemoteFile(stageCtx, client, path, interval)
	if err != nil {
		if errors.Is(err, ErrBootstrapFailed) {
			return fmt.Errorf("%w on %s%s%s", err, addr,
				bootstrapDiagnostics(ctx, client),
				bootstrapErrorLines(ctx, client))
		}

		if stageTimedOut(ctx, stageCtx) {
			return fmt.Errorf(
				"%w: waiting for the first-boot bootstrap on %s to write %s after %s: %w%s",
				ErrBringUpStageTimeout, addr, path, timeout, err,
				bootstrapDiagnostics(ctx, client),
			)
		}

		return err
	}

	b.logf("✓ First-boot bootstrap finished on %s", addr)

	return nil
}

// stageTimedOut reports whether stageCtx ended on its own deadline while the
// caller's ctx is still live, so a caller cancellation is never misreported as
// a stage timeout.
func stageTimedOut(ctx, stageCtx context.Context) bool {
	return ctx.Err() == nil && errors.Is(stageCtx.Err(), context.DeadlineExceeded)
}

// cloudInitFailed reports whether cloud-init says the node's first boot has
// finished with an unrecoverable error. Any other answer, including a probe
// that cannot run, keeps the caller waiting.
func cloudInitFailed(ctx context.Context, client *sshbootstrap.Client) bool {
	diagCtx, cancel := context.WithTimeout(ctx, bootstrapDiagnosticsTimeout)
	defer cancel()

	// cloud-init exits non-zero when it reports an error, so the output is
	// read whatever the exit status.
	result, _ := client.Run(diagCtx, bootstrapStatusCommand)

	for line := range strings.SplitSeq(string(result.Stdout), "\n") {
		if strings.TrimSpace(line) == bootstrapFailedStatus {
			return true
		}
	}

	return false
}

// bootstrapDiagnostics reads cloud-init's status off the node, formatted as a
// suffix for the returned error, or an empty string when it cannot be read.
func bootstrapDiagnostics(ctx context.Context, client *sshbootstrap.Client) string {
	return diagnosticSection(ctx, client, bootstrapDiagnosticsCommand)
}

// bootstrapErrorLines reads kubeadm's error lines from the node's cloud-init
// output log, formatted as a suffix for the returned error, or an empty string
// when there are none.
func bootstrapErrorLines(ctx context.Context, client *sshbootstrap.Client) string {
	return diagnosticSection(ctx, client, bootstrapErrorLinesCommand)
}

// diagnosticSection runs command on the node within the diagnostic deadline
// and formats its capped output as an error suffix headed by the command.
func diagnosticSection(ctx context.Context, client *sshbootstrap.Client, command string) string {
	diagCtx, cancel := context.WithTimeout(ctx, bootstrapDiagnosticsTimeout)
	defer cancel()

	// Diagnostic commands can exit non-zero while still printing what they
	// found, so the output is read whatever the exit status.
	result, _ := client.Run(diagCtx, command)

	output := strings.TrimSpace(string(result.Stdout) + string(result.Stderr))
	if output == "" {
		return ""
	}

	if len(output) > maxBootstrapDiagnosticsBytes {
		output = output[:maxBootstrapDiagnosticsBytes] + "..."
	}

	return fmt.Sprintf("\n%s:\n%s", command, output)
}
