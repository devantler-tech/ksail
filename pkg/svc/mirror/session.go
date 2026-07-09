package mirror

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// ErrCaptureWriterNil is returned when RunCaptureSession is called with a nil
// destination writer.
var ErrCaptureWriterNil = errors.New("capture writer is nil")

// ErrCaptureClientNil is returned when RunCaptureSession is called with a nil
// Kubernetes client.
var ErrCaptureClientNil = errors.New("capture client is nil")

// ErrCaptureRESTConfigNil is returned when RunCaptureSession is called with a
// nil REST config.
var ErrCaptureRESTConfigNil = errors.New("capture rest config is nil")

// CaptureExecutor is the fragment of client-go's remotecommand.Executor the
// capture session uses. It is an interface so the exec transport can be
// swapped (SPDY today, WebSockets when client-go flips its default) and
// stubbed in tests.
type CaptureExecutor interface {
	StreamWithContext(ctx context.Context, options remotecommand.StreamOptions) error
}

// CaptureExecutorFactory builds the executor that carries a capture session's
// exec stream.
type CaptureExecutorFactory func(
	config *rest.Config,
	method string,
	execURL *url.URL,
) (CaptureExecutor, error)

// CaptureSessionOption customises RunCaptureSession.
type CaptureSessionOption func(*captureSessionConfig)

type captureSessionConfig struct {
	newExecutor CaptureExecutorFactory
}

// WithCaptureExecutorFactory overrides how the session builds its exec
// transport. Production code normally leaves the SPDY default; tests use it
// to stub the stream.
func WithCaptureExecutorFactory(factory CaptureExecutorFactory) CaptureSessionOption {
	return func(cfg *captureSessionConfig) {
		if factory != nil {
			cfg.newExecutor = factory
		}
	}
}

func defaultCaptureExecutor(
	config *rest.Config,
	method string,
	execURL *url.URL,
) (CaptureExecutor, error) {
	executor, err := remotecommand.NewSPDYExecutor(config, method, execURL)
	if err != nil {
		return nil, fmt.Errorf("new spdy executor: %w", err)
	}

	return executor, nil
}

// RunCaptureSession execs the CaptureCommand tcpdump invocation inside the
// injected tap container (see InjectTap) and streams the resulting pcap bytes
// into out as they arrive — the exec channel itself carries the capture, so
// mirror mode needs no reverse tunnel. The call blocks for the lifetime of
// the capture: cancelling ctx is the intended way to stop the
// otherwise-endless tcpdump stream and is reported as a clean stop (nil),
// while any other stream failure is returned with the remote stderr attached.
func RunCaptureSession(
	ctx context.Context,
	client kubernetes.Interface,
	config *rest.Config,
	point *TapPoint,
	port int,
	out io.Writer,
	opts ...CaptureSessionOption,
) error {
	if point == nil {
		return ErrTapPointNil
	}

	if client == nil {
		return ErrCaptureClientNil
	}

	if config == nil {
		return ErrCaptureRESTConfigNil
	}

	if out == nil {
		return ErrCaptureWriterNil
	}

	command, err := CaptureCommand(port)
	if err != nil {
		return err
	}

	cfg := captureSessionConfig{newExecutor: defaultCaptureExecutor}
	for _, opt := range opts {
		opt(&cfg)
	}

	executor, err := cfg.newExecutor(config, "POST", captureExecURL(client, point, command))
	if err != nil {
		return fmt.Errorf("create capture executor: %w", err)
	}

	return streamCapture(ctx, executor, out)
}

// streamCapture runs the exec stream and maps its outcome: a cancelled
// context is the intended way to end a capture session (clean stop), any
// other stream failure is returned with the remote stderr attached.
func streamCapture(ctx context.Context, executor CaptureExecutor, out io.Writer) error {
	var stderr bytes.Buffer

	streamErr := executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: out,
		Stderr: &stderr,
	})
	if streamErr == nil {
		return nil
	}

	// StreamWithContext returns ctx.Err() verbatim when the context ends the
	// stream, so only those two errors mark the intended stop — a real exec
	// failure keeps its remote stderr even when ctx is already done.
	if errors.Is(streamErr, context.Canceled) || errors.Is(streamErr, context.DeadlineExceeded) {
		return nil
	}

	remote := strings.TrimSpace(stderr.String())
	if remote != "" {
		return fmt.Errorf("capture stream: %w (remote stderr: %s)", streamErr, remote)
	}

	return fmt.Errorf("capture stream: %w", streamErr)
}

// captureExecURL builds the exec-subresource URL that runs the capture
// command in the tap container of the tapped pod.
func captureExecURL(
	client kubernetes.Interface,
	point *TapPoint,
	command []string,
) *url.URL {
	return execURL(client, point, &corev1.PodExecOptions{
		Container: TapContainerName,
		Command:   command,
		Stdout:    true,
		Stderr:    true,
	})
}
