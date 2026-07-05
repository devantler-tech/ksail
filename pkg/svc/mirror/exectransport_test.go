package mirror_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/mirror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

const execTransportTestTimeout = 5 * time.Second

// errStubExecFailed is the terminal error an echo executor surfaces to model an
// exec stream that fails outright rather than stopping cleanly on context.
var errStubExecFailed = errors.New("stub exec failed")

// newTransportClient builds a real clientset pointed at an unreachable host.
// The exec URL is built from its RESTClient (the fake clientset's RESTClient is
// nil and panics), while the stubbed executor means the host is never dialed.
func newTransportClient(t *testing.T) kubernetes.Interface {
	t.Helper()

	client, err := kubernetes.NewForConfig(&rest.Config{Host: "https://127.0.0.1:6443"})
	require.NoError(t, err)

	return client
}

// echoExecutor implements mirror.CaptureExecutor by copying the exec session's
// stdin straight back to its stdout, modelling a container that echoes bytes.
// It returns termErr (when set) instead of the copy's outcome, and always
// honours context cancellation so Close can stop it.
type echoExecutor struct {
	termErr error
}

func (e *echoExecutor) StreamWithContext(
	ctx context.Context,
	options remotecommand.StreamOptions,
) error {
	// A terminal error models an exec stream that fails outright (not a clean
	// context-driven stop), so surface it immediately.
	if e.termErr != nil {
		return e.termErr
	}

	copyDone := make(chan error, 1)

	go func() {
		_, err := io.Copy(options.Stdout, options.Stdin)
		copyDone <- err
	}()

	select {
	case <-ctx.Done():
		return fmt.Errorf("echo executor stopped: %w", ctx.Err())
	case err := <-copyDone:
		if e.termErr != nil {
			return e.termErr
		}

		if err != nil {
			return fmt.Errorf("echo copy: %w", err)
		}

		return nil
	}
}

// tunnelEchoExecutor runs a server-role TunnelSession over the exec channel and
// echoes the first accepted stream, so a client TunnelSession layered on the
// transport exercises a full mux round-trip.
type tunnelEchoExecutor struct{}

func (tunnelEchoExecutor) StreamWithContext(
	ctx context.Context,
	options remotecommand.StreamOptions,
) error {
	peer := mirror.NewTunnelSession(options.Stdin, options.Stdout, mirror.TunnelRoleServer)

	defer func() { _ = peer.Close() }()

	stream, err := peer.AcceptStream(ctx)
	if err != nil {
		return fmt.Errorf("peer accept stream: %w", err)
	}

	_, _ = io.Copy(stream, stream)
	_ = stream.Close()

	<-ctx.Done()

	return fmt.Errorf("tunnel echo executor stopped: %w", ctx.Err())
}

func stubExecFactory(executor mirror.CaptureExecutor) mirror.CaptureExecutorFactory {
	return func(_ *rest.Config, _ string, _ *url.URL) (mirror.CaptureExecutor, error) {
		return executor, nil
	}
}

func newExecTransport(
	ctx context.Context,
	t *testing.T,
	executor mirror.CaptureExecutor,
) *mirror.ExecTransport {
	t.Helper()

	transport, err := mirror.OpenExecTransport(
		ctx,
		newTransportClient(t),
		&rest.Config{},
		&mirror.TapPoint{Namespace: "default", Pod: "app-0", Container: "app"},
		mirror.SteerContainerName,
		[]string{"ksail-steer"},
		mirror.WithCaptureExecutorFactory(stubExecFactory(executor)),
	)
	require.NoError(t, err)

	return transport
}

func TestOpenExecTransportValidation(t *testing.T) {
	t.Parallel()

	point := &mirror.TapPoint{Namespace: "default", Pod: "app-0", Container: "app"}
	command := []string{"sh"}

	tests := map[string]struct {
		point     *mirror.TapPoint
		client    bool
		config    *rest.Config
		container string
		wantErr   error
	}{
		"nil point":       {nil, true, &rest.Config{}, "app", mirror.ErrTapPointNil},
		"nil client":      {point, false, &rest.Config{}, "app", mirror.ErrExecTransportClientNil},
		"nil config":      {point, true, nil, "app", mirror.ErrExecTransportRESTConfigNil},
		"empty container": {point, true, &rest.Config{}, "", mirror.ErrExecTransportContainerEmpty},
	}

	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// A typed nil interface would defeat the nil-client guard, so pass
			// an untyped nil only when the case wants the client absent.
			var transport *mirror.ExecTransport

			var err error
			if testCase.client {
				transport, err = mirror.OpenExecTransport(
					context.Background(), newTransportClient(t), testCase.config,
					testCase.point, testCase.container, command,
				)
			} else {
				transport, err = mirror.OpenExecTransport(
					context.Background(), nil, testCase.config,
					testCase.point, testCase.container, command,
				)
			}

			require.ErrorIs(t, err, testCase.wantErr)
			assert.Nil(t, transport)
		})
	}
}

func TestExecTransportBidirectionalEcho(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), execTransportTestTimeout)
	defer cancel()

	transport := newExecTransport(ctx, t, &echoExecutor{})

	defer func() { _ = transport.Close() }()

	payload := []byte("ping-pong")
	writeErr := make(chan error, 1)

	go func() {
		_, err := transport.Write(payload)
		writeErr <- err
	}()

	got := make([]byte, len(payload))
	_, err := io.ReadFull(transport, got)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
	require.NoError(t, <-writeErr)
}

func TestExecTransportBacksTunnelSession(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), execTransportTestTimeout)
	defer cancel()

	transport := newExecTransport(ctx, t, tunnelEchoExecutor{})

	defer func() { _ = transport.Close() }()

	session := mirror.NewTunnelSession(transport, transport, mirror.TunnelRoleClient)

	defer func() { _ = session.Close() }()

	stream, err := session.OpenStream()
	require.NoError(t, err)

	payload := []byte("intercept-me")

	_, err = stream.Write(payload)
	require.NoError(t, err)

	got := make([]byte, len(payload))
	_, err = io.ReadFull(stream, got)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
}

func TestExecTransportCloseSurfacesStreamError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), execTransportTestTimeout)
	defer cancel()

	transport := newExecTransport(ctx, t, &echoExecutor{termErr: errStubExecFailed})

	// Nothing is written, so the echo copy never returns on its own; Close
	// cancels the stream and must still surface the executor's terminal error.
	closeErr := transport.Close()
	require.ErrorIs(t, closeErr, errStubExecFailed)
}

func TestExecTransportCloseUnblocksRead(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), execTransportTestTimeout)
	defer cancel()

	transport := newExecTransport(ctx, t, &echoExecutor{})
	readReturned := make(chan error, 1)

	go func() {
		_, err := transport.Read(make([]byte, 8))
		readReturned <- err
	}()

	// Give the reader a moment to park, then close and require it unblocks.
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, transport.Close())

	select {
	case <-readReturned:
	case <-time.After(execTransportTestTimeout):
		t.Fatal("Read did not unblock after Close")
	}
}
