package sshbootstrap_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"testing"
	"time"

	sshbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/ssh"
	"golang.org/x/crypto/ssh"
)

const (
	testUser          = "root"
	testDialTimeout   = 5 * time.Second
	testRetryInterval = 30 * time.Millisecond
	testCancelBudget  = 250 * time.Millisecond
	testRunBudget     = 5 * time.Second
)

var errUnknownClientKey = errors.New("unknown client key")

// execHandler scripts the in-process server's response to one exec request.
type execHandler func(command string) (stdout string, stderr string, exitCode uint32)

// exitStatusPayload is the SSH exit-status request payload (RFC 4254 §6.10).
type exitStatusPayload struct {
	Status uint32
}

// startServer runs a minimal in-process SSH server that authenticates
// clientKey, answers exec requests via handler, and rejects the first
// rejectFirst TCP connections outright (to exercise retry). It returns the
// listen address and the server's host key.
func startServer(
	t *testing.T,
	clientKey ssh.PublicKey,
	handler execHandler,
	rejectFirst int,
) (string, ssh.PublicKey) {
	t.Helper()

	hostPair, err := sshbootstrap.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}

	config := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if !bytes.Equal(key.Marshal(), clientKey.Marshal()) {
				return nil, errUnknownClientKey
			}

			return &ssh.Permissions{}, nil
		},
	}
	config.AddHostKey(hostPair.Signer)

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	t.Cleanup(func() { _ = listener.Close() })

	go acceptLoop(listener, config, handler, rejectFirst)

	return listener.Addr().String(), hostPair.Signer.PublicKey()
}

// acceptLoop serves connections until the listener closes.
func acceptLoop(
	listener net.Listener,
	config *ssh.ServerConfig,
	handler execHandler,
	rejectFirst int,
) {
	rejected := 0

	for {
		netConn, err := listener.Accept()
		if err != nil {
			return
		}

		if rejected < rejectFirst {
			rejected++

			_ = netConn.Close()

			continue
		}

		go serveConn(netConn, config, handler)
	}
}

// serveConn handles one SSH connection: session channels with exec requests.
func serveConn(netConn net.Conn, config *ssh.ServerConfig, handler execHandler) {
	_, channels, requests, err := ssh.NewServerConn(netConn, config)
	if err != nil {
		return
	}

	go ssh.DiscardRequests(requests)

	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "only sessions are supported")

			continue
		}

		channel, channelRequests, err := newChannel.Accept()
		if err != nil {
			continue
		}

		go serveSession(channel, channelRequests, handler)
	}
}

// execRequestPayload is the SSH exec request payload (RFC 4254 §6.5).
type execRequestPayload struct {
	Command string
}

// serveSession answers exec requests on one session channel.
func serveSession(channel ssh.Channel, requests <-chan *ssh.Request, handler execHandler) {
	defer func() { _ = channel.Close() }()

	for request := range requests {
		if request.Type != "exec" {
			_ = request.Reply(false, nil)

			continue
		}

		var payload execRequestPayload

		err := ssh.Unmarshal(request.Payload, &payload)
		if err != nil {
			_ = request.Reply(false, nil)

			continue
		}

		_ = request.Reply(true, nil)

		stdout, stderr, exitCode := handler(payload.Command)

		_, _ = channel.Write([]byte(stdout))
		_, _ = channel.Stderr().Write([]byte(stderr))
		_, _ = channel.SendRequest(
			"exit-status", false, ssh.Marshal(exitStatusPayload{Status: exitCode}),
		)

		return
	}
}

// dialOptions builds client options against the test server.
func dialOptions(
	addr string,
	pair sshbootstrap.KeyPair,
	hostKey ssh.PublicKey,
) sshbootstrap.Options {
	return sshbootstrap.Options{
		Addr:            addr,
		User:            testUser,
		Signer:          pair.Signer,
		HostKeyCallback: ssh.FixedHostKey(hostKey),
		DialTimeout:     testDialTimeout,
	}
}

// mustDial connects to the test server or fails the test.
func mustDial(
	t *testing.T,
	addr string,
	pair sshbootstrap.KeyPair,
	hostKey ssh.PublicKey,
) *sshbootstrap.Client {
	t.Helper()

	client, err := sshbootstrap.Dial(t.Context(), dialOptions(addr, pair, hostKey))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	t.Cleanup(func() { _ = client.Close() })

	return client
}

// mustGenerateKeyPair mints a client keypair or fails the test.
func mustGenerateKeyPair(t *testing.T) sshbootstrap.KeyPair {
	t.Helper()

	pair, err := sshbootstrap.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}

	return pair
}

func TestFileExists(t *testing.T) {
	t.Parallel()

	pair := mustGenerateKeyPair(t)
	addr, hostKey := startServer(
		t,
		pair.Signer.PublicKey(),
		func(command string) (string, string, uint32) {
			switch command {
			case "test -f '/present'":
				return "", "", 0
			case "test -f '/absent'":
				return "", "", 1
			default:
				return "", "", 2
			}
		},
		0,
	)

	client := mustDial(t, addr, pair, hostKey)

	exists, err := client.FileExists(t.Context(), "/present")
	if err != nil || !exists {
		t.Fatalf("present file: got exists=%v err=%v, want true, nil", exists, err)
	}

	exists, err = client.FileExists(t.Context(), "/absent")
	if err != nil || exists {
		t.Fatalf("absent file: got exists=%v err=%v, want false, nil", exists, err)
	}

	_, err = client.FileExists(t.Context(), "/probe-error")
	if err == nil {
		t.Fatal("unexpected exit code: want an error, got nil")
	}
}

func TestGenerateKeyPairRoundTrips(t *testing.T) {
	t.Parallel()

	pair := mustGenerateKeyPair(t)

	parsedPub, _, _, rest, err := ssh.ParseAuthorizedKey([]byte(pair.AuthorizedKey + "\n"))
	if err != nil {
		t.Fatalf("authorized key does not parse: %v", err)
	}

	if len(rest) != 0 {
		t.Fatalf("authorized key has trailing data: %q", rest)
	}

	if !bytes.Equal(parsedPub.Marshal(), pair.Signer.PublicKey().Marshal()) {
		t.Fatal("authorized key does not match the signer's public key")
	}

	restored, err := sshbootstrap.ParsePrivateKey(pair.PrivateKeyPEM)
	if err != nil {
		t.Fatalf("PEM round-trip failed: %v", err)
	}

	if restored.AuthorizedKey != pair.AuthorizedKey {
		t.Fatalf(
			"round-tripped public key mismatch: got %q, want %q",
			restored.AuthorizedKey, pair.AuthorizedKey,
		)
	}
}

func TestParsePrivateKeyRejectsGarbage(t *testing.T) {
	t.Parallel()

	_, err := sshbootstrap.ParsePrivateKey([]byte("not a key"))
	if err == nil {
		t.Fatal("expected an error for garbage input")
	}
}

func TestDialValidatesOptions(t *testing.T) {
	t.Parallel()

	pair := mustGenerateKeyPair(t)
	hostKeyCallback := ssh.FixedHostKey(pair.Signer.PublicKey())

	tests := []struct {
		name    string
		opts    sshbootstrap.Options
		wantErr error
	}{
		{
			name: "missing addr",
			opts: sshbootstrap.Options{
				User:            testUser,
				Signer:          pair.Signer,
				HostKeyCallback: hostKeyCallback,
			},
			wantErr: sshbootstrap.ErrMissingAddr,
		},
		{
			name: "missing user",
			opts: sshbootstrap.Options{
				Addr:            "127.0.0.1:22",
				Signer:          pair.Signer,
				HostKeyCallback: hostKeyCallback,
			},
			wantErr: sshbootstrap.ErrMissingUser,
		},
		{
			name: "missing signer",
			opts: sshbootstrap.Options{
				Addr:            "127.0.0.1:22",
				User:            testUser,
				HostKeyCallback: hostKeyCallback,
			},
			wantErr: sshbootstrap.ErrMissingSigner,
		},
		{
			name: "missing host key callback",
			opts: sshbootstrap.Options{
				Addr:   "127.0.0.1:22",
				User:   testUser,
				Signer: pair.Signer,
			},
			wantErr: sshbootstrap.ErrMissingHostKeyCallback,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := sshbootstrap.Dial(t.Context(), testCase.opts)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("got %v, want %v", err, testCase.wantErr)
			}
		})
	}
}

func TestRunCapturesOutputAndZeroExit(t *testing.T) {
	t.Parallel()

	pair := mustGenerateKeyPair(t)
	addr, hostKey := startServer(
		t,
		pair.Signer.PublicKey(),
		func(command string) (string, string, uint32) {
			if command != "uname -r" {
				return "", "unexpected command: " + command, 1
			}

			return "6.8.0-generic\n", "", 0
		},
		0,
	)

	client := mustDial(t, addr, pair, hostKey)

	ctx, cancel := context.WithTimeout(t.Context(), testRunBudget)
	defer cancel()

	result, err := client.Run(ctx, "uname -r")
	if err != nil {
		t.Fatalf("run: %v (stderr: %s)", err, result.Stderr)
	}

	if got, want := string(result.Stdout), "6.8.0-generic\n"; got != want {
		t.Fatalf("stdout: got %q, want %q", got, want)
	}

	if result.ExitCode != 0 {
		t.Fatalf("exit code: got %d, want 0", result.ExitCode)
	}
}

func TestRunSurfacesNonZeroExit(t *testing.T) {
	t.Parallel()

	pair := mustGenerateKeyPair(t)
	addr, hostKey := startServer(t, pair.Signer.PublicKey(), func(string) (string, string, uint32) {
		return "", "boom\n", 7
	}, 0)

	client := mustDial(t, addr, pair, hostKey)

	ctx, cancel := context.WithTimeout(t.Context(), testRunBudget)
	defer cancel()

	result, err := client.Run(ctx, "false")
	if !errors.Is(err, sshbootstrap.ErrCommandFailed) {
		t.Fatalf("got %v, want ErrCommandFailed", err)
	}

	if result.ExitCode != 7 {
		t.Fatalf("exit code: got %d, want 7", result.ExitCode)
	}

	if got, want := string(result.Stderr), "boom\n"; got != want {
		t.Fatalf("stderr: got %q, want %q", got, want)
	}
}

func TestReadFileFetchesQuotedPath(t *testing.T) {
	t.Parallel()

	const kubeconfig = "apiVersion: v1\nkind: Config\n"

	var sawCommand string

	pair := mustGenerateKeyPair(t)
	addr, hostKey := startServer(
		t,
		pair.Signer.PublicKey(),
		func(command string) (string, string, uint32) {
			sawCommand = command

			return kubeconfig, "", 0
		},
		0,
	)

	client := mustDial(t, addr, pair, hostKey)

	ctx, cancel := context.WithTimeout(t.Context(), testRunBudget)
	defer cancel()

	got, err := client.ReadFile(ctx, "/etc/rancher/k3s/k3s.yaml")
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	if string(got) != kubeconfig {
		t.Fatalf("content: got %q, want %q", got, kubeconfig)
	}

	if want := "cat -- '/etc/rancher/k3s/k3s.yaml'"; sawCommand != want {
		t.Fatalf("command: got %q, want %q", sawCommand, want)
	}
}

func TestReadFileEscapesSingleQuotes(t *testing.T) {
	t.Parallel()

	var sawCommand string

	pair := mustGenerateKeyPair(t)
	addr, hostKey := startServer(
		t,
		pair.Signer.PublicKey(),
		func(command string) (string, string, uint32) {
			sawCommand = command

			return "", "", 0
		},
		0,
	)

	client := mustDial(t, addr, pair, hostKey)

	ctx, cancel := context.WithTimeout(t.Context(), testRunBudget)
	defer cancel()

	_, err := client.ReadFile(ctx, "/tmp/o'clock")
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	if want := `cat -- '/tmp/o'\''clock'`; sawCommand != want {
		t.Fatalf("command: got %q, want %q", sawCommand, want)
	}
}

func TestDialRejectsUnknownHostKey(t *testing.T) {
	t.Parallel()

	pair := mustGenerateKeyPair(t)
	otherPair := mustGenerateKeyPair(t)

	addr, _ := startServer(t, pair.Signer.PublicKey(), func(string) (string, string, uint32) {
		return "", "", 0
	}, 0)

	// Pin the WRONG host key: the handshake must fail.
	_, err := sshbootstrap.Dial(
		t.Context(),
		dialOptions(addr, pair, otherPair.Signer.PublicKey()),
	)
	if err == nil {
		t.Fatal("expected a host-key verification error")
	}
}

func TestDialWithRetryRecoversFromEarlyRefusals(t *testing.T) {
	t.Parallel()

	pair := mustGenerateKeyPair(t)
	addr, hostKey := startServer(t, pair.Signer.PublicKey(), func(string) (string, string, uint32) {
		return "up\n", "", 0
	}, 2)

	ctx, cancel := context.WithTimeout(t.Context(), testRunBudget)
	defer cancel()

	client, err := sshbootstrap.DialWithRetry(
		ctx, dialOptions(addr, pair, hostKey), testRetryInterval,
	)
	if err != nil {
		t.Fatalf("dial with retry: %v", err)
	}

	t.Cleanup(func() { _ = client.Close() })
}

func TestDialWithRetryHonoursContextDeadline(t *testing.T) {
	t.Parallel()

	pair := mustGenerateKeyPair(t)

	// Reserve a port, then close it so every attempt is refused.
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	addr := listener.Addr().String()
	_ = listener.Close()

	ctx, cancel := context.WithTimeout(t.Context(), testCancelBudget)
	defer cancel()

	_, err = sshbootstrap.DialWithRetry(
		ctx,
		dialOptions(addr, pair, pair.Signer.PublicKey()),
		testRetryInterval,
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want context.DeadlineExceeded", err)
	}
}

func TestDialWithRetryStopsOnOptionError(t *testing.T) {
	t.Parallel()

	started := time.Now()

	_, err := sshbootstrap.DialWithRetry(
		t.Context(),
		sshbootstrap.Options{Addr: "127.0.0.1:22", User: testUser},
		testRetryInterval,
	)
	if !errors.Is(err, sshbootstrap.ErrMissingSigner) {
		t.Fatalf("got %v, want ErrMissingSigner", err)
	}

	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("option error should fail fast, took %s", elapsed)
	}
}
