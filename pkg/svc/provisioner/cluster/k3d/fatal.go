package k3dprovisioner

import (
	"errors"
	"fmt"
	"strings"

	"github.com/k3d-io/k3d/v5/pkg/runtimes"
	"github.com/sirupsen/logrus"
)

// ErrK3dCommandFailed is returned when an embedded k3d command stops with a fatal
// error while its runtime is reachable. The error carries k3d's own message, so
// the reason is in the error itself rather than only in the log lines printed
// before it.
var ErrK3dCommandFailed = errors.New("k3d command failed")

// dockerUnreachableMessage is the text the Docker client puts in every error for a
// daemon it cannot connect to ("Cannot connect to the Docker daemon at <host>. Is
// the docker daemon running?"). When k3d's fatal message already contains it, the
// runtime is known to be unreachable without checking again.
const dockerUnreachableMessage = "cannot connect to the docker daemon"

// k3dFatal is the panic value k3dFatalHook raises when k3d logs at fatal level.
// It carries k3d's message to runK3dSafely, which recovers it into an error.
type k3dFatal struct {
	message string
}

// k3dFatalHook stops a k3d fatal log entry from exiting the process while keeping
// its message. logrus fires hooks before it calls ExitFunc, and it fires them on
// the goroutine that logged, so panicking here hands the message to that
// goroutine's runK3dSafely without any state shared between concurrent commands.
// No logger lock is held while hooks fire.
type k3dFatalHook struct{}

// Levels limits the hook to fatal entries.
func (k3dFatalHook) Levels() []logrus.Level {
	return []logrus.Level{logrus.FatalLevel}
}

// Fire raises the fatal entry's message as a k3dFatal panic.
func (k3dFatalHook) Fire(entry *logrus.Entry) error {
	panic(k3dFatal{message: entry.Message})
}

// defaultRuntimeReachable asks the runtime k3d itself uses (the Docker daemon, as
// resolved from the same environment) for its info, returning the error when it
// cannot be reached.
func defaultRuntimeReachable() error {
	_, err := runtimes.SelectedRuntime.Info()
	if err != nil {
		return fmt.Errorf("checking the k3d runtime: %w", err)
	}

	return nil
}

// fatalError turns the message k3d logged at fatal level into the error
// runK3dSafely returns. k3d often logs the runtime's own error at a lower level
// and then exits with a generic message such as "No nodes found for given
// cluster", so the message alone cannot tell whether Docker was reachable. The
// error is ErrK3dRuntimeUnavailable, with its Docker hint, only when the message
// shows the daemon could not be reached or the runtime does not answer now;
// otherwise it is ErrK3dCommandFailed with k3d's reason.
func (k *Provisioner) fatalError(message string) error {
	message = strings.TrimSpace(message)

	if strings.Contains(strings.ToLower(message), dockerUnreachableMessage) {
		return fmt.Errorf("%w: %s", ErrK3dRuntimeUnavailable, message)
	}

	runtimeReachable := k.runtimeReachable
	if runtimeReachable == nil {
		runtimeReachable = defaultRuntimeReachable
	}

	probeErr := runtimeReachable()
	if probeErr != nil {
		if message == "" {
			return fmt.Errorf("%w: %w", ErrK3dRuntimeUnavailable, probeErr)
		}

		return fmt.Errorf("%w: %s: %w", ErrK3dRuntimeUnavailable, message, probeErr)
	}

	if message == "" {
		return ErrK3dCommandFailed
	}

	return fmt.Errorf("%w: %s", ErrK3dCommandFailed, message)
}
