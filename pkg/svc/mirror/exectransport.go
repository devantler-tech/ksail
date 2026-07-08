package mirror

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// ErrExecTransportClientNil is returned when OpenExecTransport is called with
// a nil Kubernetes client.
var ErrExecTransportClientNil = errors.New("exec transport client is nil")

// ErrExecTransportRESTConfigNil is returned when OpenExecTransport is called
// with a nil REST config.
var ErrExecTransportRESTConfigNil = errors.New("exec transport rest config is nil")

// ErrExecTransportContainerEmpty is returned when OpenExecTransport is called
// without a container name.
var ErrExecTransportContainerEmpty = errors.New("exec transport container is empty")

// ErrExecTransportClosed is the error a blocked stdout write is unblocked with
// when the transport is closed before the stream ends on its own.
var ErrExecTransportClosed = errors.New("exec transport closed")

// ExecTransport is a bidirectional byte stream over a container exec session:
// Read yields the container's stdout and Write feeds its stdin. It satisfies
// io.ReadWriteCloser so it can back a TunnelSession (see NewTunnelSession) —
// the mux the intercept path runs the steering tunnel over. Unlike the capture
// session (stdout-only, see RunCaptureSession), the intercept tunnel needs both
// directions, so this bridges the exec channel's stdin and stdout onto pipes.
type ExecTransport struct {
	stdinReader  *io.PipeReader
	stdinWriter  *io.PipeWriter
	stdoutReader *io.PipeReader
	stdoutWriter *io.PipeWriter
	cancel       context.CancelFunc
	done         chan struct{}
	closeOnce    sync.Once
	mu           sync.Mutex
	streamErr    error
}

// OpenExecTransport opens an exec session running command in the named
// container of point.Pod and returns a bidirectional transport whose Read and
// Write are the session's stdout and stdin. The exec runs in a background
// goroutine for the life of the transport; Close (or the stream ending on its
// own) tears it down and reports the terminal stream error. It reuses the
// shared exec-executor seam (CaptureExecutor / WithCaptureExecutorFactory) so
// tests can stub the stream without a live cluster.
func OpenExecTransport(
	ctx context.Context,
	client kubernetes.Interface,
	config *rest.Config,
	point *TapPoint,
	container string,
	command []string,
	opts ...CaptureSessionOption,
) (*ExecTransport, error) {
	if point == nil {
		return nil, ErrTapPointNil
	}

	if client == nil {
		return nil, ErrExecTransportClientNil
	}

	if config == nil {
		return nil, ErrExecTransportRESTConfigNil
	}

	if container == "" {
		return nil, ErrExecTransportContainerEmpty
	}

	cfg := captureSessionConfig{newExecutor: defaultCaptureExecutor}
	for _, opt := range opts {
		opt(&cfg)
	}

	execURL := execStreamURL(client, point, container, command)

	executor, err := cfg.newExecutor(config, "POST", execURL)
	if err != nil {
		return nil, fmt.Errorf("create exec transport executor: %w", err)
	}

	// cancel is stored on the transport and invoked in Close (the type's
	// io.Closer contract requires Close); gosec G118 cannot track that transfer.

	streamCtx, cancel := context.WithCancel(ctx)
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	transport := &ExecTransport{
		stdinReader:  stdinReader,
		stdinWriter:  stdinWriter,
		stdoutReader: stdoutReader,
		stdoutWriter: stdoutWriter,
		cancel:       cancel,
		done:         make(chan struct{}),
	}

	go transport.run(streamCtx, executor)

	return transport, nil
}

// Read yields bytes the container wrote to the exec session's stdout.
func (t *ExecTransport) Read(data []byte) (int, error) {
	//nolint:wrapcheck // io.PipeReader errors (io.EOF / io.ErrClosedPipe) are the transport's own contract.
	return t.stdoutReader.Read(data)
}

// Write feeds bytes into the exec session's stdin.
func (t *ExecTransport) Write(data []byte) (int, error) {
	//nolint:wrapcheck // io.PipeWriter errors (io.ErrClosedPipe) are the transport's own contract.
	return t.stdinWriter.Write(data)
}

// Close ends the exec stream, unblocks any parked Read/Write, waits for the
// carrier goroutine to finish, and returns the terminal stream error (nil when
// the stream ended cleanly or was stopped by Close/context cancellation).
func (t *ExecTransport) Close() error {
	t.closeOnce.Do(func() {
		t.cancel()
		// Unblock a carrier goroutine parked writing stdout into the pipe, and
		// signal EOF to the exec stdin so the remote command sees the close.
		_ = t.stdoutReader.CloseWithError(ErrExecTransportClosed)
		_ = t.stdinWriter.Close()
	})

	<-t.done

	t.mu.Lock()
	defer t.mu.Unlock()

	return t.streamErr
}

// run carries the exec stream for the transport's lifetime and, on exit, closes
// both pipe ends so any blocked Read/Write unblocks. A context cancellation is
// the intended way to stop the stream and is normalised to a clean shutdown.
func (t *ExecTransport) run(ctx context.Context, executor CaptureExecutor) {
	defer close(t.done)
	// Release the WithCancel child unconditionally: when the stream ends on its
	// own (Close never called) the child would otherwise stay registered on the
	// parent until it cancels. cancel is idempotent, so this is safe alongside Close.
	defer t.cancel()

	streamErr := executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  t.stdinReader,
		Stdout: t.stdoutWriter,
		Stderr: io.Discard,
	})

	// StreamWithContext returns ctx.Err() verbatim when the context ends the
	// stream, which Close triggers on purpose — treat it as a clean stop.
	if errors.Is(streamErr, context.Canceled) || errors.Is(streamErr, context.DeadlineExceeded) {
		streamErr = nil
	}

	t.mu.Lock()
	t.streamErr = streamErr
	t.mu.Unlock()

	// Unblock a caller parked in Read (stdout EOF) and in Write (stdin closed).
	_ = t.stdoutWriter.CloseWithError(io.EOF)
	_ = t.stdinReader.CloseWithError(io.EOF)
}

// execURL builds the exec-subresource URL for the tapped pod, running the
// given PodExecOptions in one of its containers. It is the single home for the
// pods/exec REST-request chain shared by the stream and capture builders.
func execURL(client kubernetes.Interface, point *TapPoint, opts *corev1.PodExecOptions) *url.URL {
	return client.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(point.Pod).
		Namespace(point.Namespace).
		SubResource("exec").
		VersionedParams(opts, scheme.ParameterCodec).
		URL()
}

// execStreamURL builds the exec-subresource URL for a bidirectional stream
// (stdin + stdout) running command in the named container of the tapped pod.
func execStreamURL(
	client kubernetes.Interface,
	point *TapPoint,
	container string,
	command []string,
) *url.URL {
	return execURL(client, point, &corev1.PodExecOptions{
		Container: container,
		Command:   command,
		Stdin:     true,
		Stdout:    true,
		Stderr:    true,
	})
}
