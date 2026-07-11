package steeragent_test

import (
	"context"
	"errors"
	"io"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/steeragent"
)

const waitTimeout = 2 * time.Second

var (
	errListenNotExpected = errors.New("seam must not be called")
	errBoom              = errors.New("boom")
)

// fakeTransport is an in-memory ReadWriteCloser whose Read blocks until Close,
// standing in for the exec channel without a live peer.
type fakeTransport struct {
	closeOnce sync.Once
	done      chan struct{}
}

func newFakeTransport() *fakeTransport { return &fakeTransport{done: make(chan struct{})} }

func (t *fakeTransport) Read(_ []byte) (int, error) {
	<-t.done

	return 0, io.EOF
}

func (t *fakeTransport) Write(p []byte) (int, error) { return len(p), nil }

func (t *fakeTransport) Close() error {
	t.closeOnce.Do(func() { close(t.done) })

	return nil
}

// blockingListener is a net.Listener whose Accept blocks until Close, so the
// agent parks on it exactly as it would on a real listener with no inbound
// connections.
type blockingListener struct {
	closeOnce sync.Once
	closed    chan struct{}
}

func newBlockingListener() *blockingListener { return &blockingListener{closed: make(chan struct{})} }

func (l *blockingListener) Accept() (net.Conn, error) {
	<-l.closed

	return nil, net.ErrClosed
}

func (l *blockingListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })

	return nil
}

func (l *blockingListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4zero, Port: 0}
}

// callRecorder captures the iptables commands the agent runs and signals when
// the install rule (`-I`) has been applied.
type callRecorder struct {
	mu       sync.Mutex
	calls    [][]string
	inserted chan struct{}
	once     sync.Once
}

func newCallRecorder() *callRecorder { return &callRecorder{inserted: make(chan struct{})} }

func (rec *callRecorder) run(_ context.Context, name string, args ...string) error {
	rec.mu.Lock()
	rec.calls = append(rec.calls, append([]string{name}, args...))
	rec.mu.Unlock()

	if slices.Contains(args, "-I") {
		rec.once.Do(func() { close(rec.inserted) })
	}

	return nil
}

func (rec *callRecorder) assertInstallThenRemove(t *testing.T) {
	t.Helper()

	rec.mu.Lock()
	defer rec.mu.Unlock()

	if len(rec.calls) < 2 {
		t.Fatalf("expected an install and a teardown call, got %d: %v", len(rec.calls), rec.calls)
	}

	first := rec.calls[0]
	if !slices.Contains(first, "-I") || !slices.Contains(first, "8080") {
		t.Errorf("first call should install (-I) service port 8080, got %v", first)
	}

	last := rec.calls[len(rec.calls)-1]
	if !slices.Contains(last, "-D") || !slices.Contains(last, "8080") {
		t.Errorf("last call should delete (-D) service port 8080, got %v", last)
	}
}

func waitFor(t *testing.T, signal <-chan struct{}, msg string) {
	t.Helper()

	select {
	case <-signal:
	case <-time.After(waitTimeout):
		t.Fatal(msg)
	}
}

func assertGracefulStop(t *testing.T, done <-chan error) {
	t.Helper()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned an error on graceful stop: %v", err)
		}
	case <-time.After(waitTimeout):
		t.Fatal("run did not return after context cancellation")
	}
}

func TestRunForTest_InstallsThenRemovesRedirectRule(t *testing.T) {
	t.Parallel()

	recorder := newCallRecorder()
	transport := newFakeTransport()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- steeragent.RunForTest(ctx, 8080, 19000, transport,
			func(context.Context, int) (net.Listener, error) { return newBlockingListener(), nil },
			recorder.run)
	}()

	waitFor(t, recorder.inserted, "the redirect install rule was never run")
	cancel()
	assertGracefulStop(t, done)

	_ = transport.Close()

	recorder.assertInstallThenRemove(t)
}

func TestRunForTest_RejectsInvalidPorts(t *testing.T) {
	t.Parallel()

	err := steeragent.RunForTest(context.Background(), 0, 19000, newFakeTransport(),
		func(context.Context, int) (net.Listener, error) {
			t.Error("listen must not be called when validation fails")

			return nil, errListenNotExpected
		},
		func(context.Context, string, ...string) error {
			t.Error("runner must not be called when validation fails")

			return errListenNotExpected
		})
	if err == nil {
		t.Fatal("expected a validation error for a zero service port")
	}
}

func TestRunForTest_PropagatesListenError(t *testing.T) {
	t.Parallel()

	err := steeragent.RunForTest(context.Background(), 8080, 19000, newFakeTransport(),
		func(context.Context, int) (net.Listener, error) { return nil, errBoom },
		func(context.Context, string, ...string) error {
			t.Error("runner must not be called when the listener fails to open")

			return nil
		})
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected the wrapped listen error, got %v", err)
	}
}

func TestRunForTest_PropagatesInstallError(t *testing.T) {
	t.Parallel()

	err := steeragent.RunForTest(context.Background(), 8080, 19000, newFakeTransport(),
		func(context.Context, int) (net.Listener, error) { return newBlockingListener(), nil },
		func(context.Context, string, ...string) error { return errBoom })
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected the wrapped install error, got %v", err)
	}
}

func TestListenIntercept_BindsAllIPv4Interfaces(t *testing.T) {
	t.Parallel()

	listener, err := steeragent.ListenInterceptForTest(context.Background(), 0)
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}

	defer func() { _ = listener.Close() }()

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("expected a *net.TCPAddr, got %T", listener.Addr())
	}

	// REDIRECT delivers remote traffic to the pod IP, not loopback (#6039), so
	// the listener must bind every IPv4 interface; the agent's iptables guard
	// rule re-establishes least exposure.
	if !tcpAddr.IP.Equal(net.IPv4zero) {
		t.Errorf("listener bound %s, want 0.0.0.0 (all IPv4 interfaces)", tcpAddr.IP)
	}
}

func TestNewSteerAgentCmd(t *testing.T) {
	t.Parallel()

	cmd := steeragent.NewSteerAgentCmd()

	if cmd.Use != "steer-agent" {
		t.Errorf("Use = %q, want %q", cmd.Use, "steer-agent")
	}

	if !cmd.Hidden {
		t.Error("the steer-agent command must be hidden (internal entrypoint)")
	}

	if cmd.Annotations[annotations.AnnotationExclude] != "true" {
		t.Error("the steer-agent command must be excluded from the tool surface")
	}

	for _, name := range []string{"service-port", "intercept-port"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing --%s flag", name)
		}
	}
}
