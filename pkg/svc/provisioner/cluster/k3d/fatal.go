package k3dprovisioner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/docker/docker/api/types/system"
	k3ddocker "github.com/k3d-io/k3d/v5/pkg/runtimes/docker"
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

// runtimeProbeTimeout bounds the runtime check, so a daemon that accepts the
// connection but never answers cannot hold up the error (or, during List, the
// output redirection and listMutex that List releases only after it returns).
const runtimeProbeTimeout = 10 * time.Second

// runtimeInfoClient is the part of a Docker client the runtime check uses.
type runtimeInfoClient interface {
	Info(ctx context.Context) (system.Info, error)
	Close() error
}

// newK3dRuntimeClient creates k3d's own Docker client, so the check reaches the
// daemon that the same environment selects for k3d.
func newK3dRuntimeClient() (runtimeInfoClient, error) {
	dockerClient, err := k3ddocker.GetDockerClient()
	if err != nil {
		return nil, fmt.Errorf("getting k3d's Docker client: %w", err)
	}

	return dockerClient, nil
}

// defaultRuntimeReachable asks the Docker daemon k3d itself uses for its info
// within runtimeProbeTimeout. It returns the error when the daemon cannot be
// reached or does not answer in time.
func defaultRuntimeReachable() error {
	return runtimeReachableWithin(newK3dRuntimeClient, runtimeProbeTimeout)
}

// runtimeReachableWithin asks the client newClient creates for the daemon's info
// within timeout. The limit is a context deadline, so it cancels the request
// itself.
func runtimeReachableWithin(
	newClient func() (runtimeInfoClient, error),
	timeout time.Duration,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	runtimeClient, err := newClient()
	if err != nil {
		return fmt.Errorf("creating the k3d runtime client: %w", err)
	}

	defer func() { _ = runtimeClient.Close() }()

	_, err = runtimeClient.Info(ctx)
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
