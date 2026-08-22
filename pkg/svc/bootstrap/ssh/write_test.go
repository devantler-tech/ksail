package sshbootstrap_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	sshbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/ssh"
)

// payloadMarker is a token that must never reach the remote command line. It
// stands in for cluster signing material without being a usable key.
const payloadMarker = "MARKER-cbf29ce484222325"

// writeRecorder captures what the stubbed server observed for one write, so a
// test can assert where the content travelled rather than only that the call
// succeeded.
type writeRecorder struct {
	mu      sync.Mutex
	command string
	stdin   []byte
}

func (r *writeRecorder) record(command string, stdin []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.command = command

	r.stdin = append([]byte(nil), stdin...)
}

func (r *writeRecorder) snapshot() (string, []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.command, append([]byte(nil), r.stdin...)
}

// writeServer dials a stub that records the write it saw and answers with the
// scripted exit code and stderr.
func writeServer(
	t *testing.T,
	exitCode uint32,
	stderr string,
) (*sshbootstrap.Client, *writeRecorder) {
	t.Helper()

	pair := mustGenerateKeyPair(t)
	recorder := &writeRecorder{}

	addr, hostKey := startServer(
		t,
		pair.Signer.PublicKey(),
		func(command string, stdin []byte) (string, string, uint32) {
			recorder.record(command, stdin)

			return "", stderr, exitCode
		},
		0,
	)

	return mustDial(t, addr, pair, hostKey), recorder
}

// TestWriteFileKeepsContentOutOfTheCommandLine pins the security property the
// primitive exists for: secret bytes travel on stdin, so they never land in the
// node's process arguments or shell history.
func TestWriteFileKeepsContentOutOfTheCommandLine(t *testing.T) {
	t.Parallel()

	secret := []byte(
		"-----BEGIN PRIVATE KEY-----\n" + payloadMarker + "\n-----END PRIVATE KEY-----\n",
	)
	client, recorder := writeServer(t, 0, "")

	err := client.WriteFile(t.Context(), "/etc/kubernetes/pki/ca.key", secret, 0o600)
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	command, stdin := recorder.snapshot()

	if string(stdin) != string(secret) {
		t.Fatalf("stdin: got %q, want the file content", stdin)
	}

	if strings.Contains(command, payloadMarker) {
		t.Fatalf("secret content reached the command line: %q", command)
	}

	if !strings.Contains(command, "/etc/kubernetes/pki/ca.key") {
		t.Fatalf("command does not name the destination: %q", command)
	}

	// The remote side must be able to detect a short transfer, which means the
	// expected byte count has to cross with the request.
	if !strings.Contains(command, strconv.Itoa(len(secret))) {
		t.Fatalf("command carries no expected byte count: %q", command)
	}
}

// TestWriteFileAppliesTheRequestedMode asserts the caller's mode is the mode
// applied, rather than whatever umask the login shell happens to carry.
func TestWriteFileAppliesTheRequestedMode(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		mode fs.FileMode
		want string
	}{
		"explicit owner only":   {mode: 0o600, want: "0600"},
		"group readable":        {mode: 0o640, want: "0640"},
		"zero means owner only": {mode: 0, want: "0600"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client, recorder := writeServer(t, 0, "")

			err := client.WriteFile(t.Context(), "/tmp/material", []byte("x"), testCase.mode)
			if err != nil {
				t.Fatalf("write: %v", err)
			}

			command, _ := recorder.snapshot()

			if !strings.Contains(command, "chmod "+testCase.want) {
				t.Fatalf("mode %v: want chmod %s in %q", testCase.mode, testCase.want, command)
			}
		})
	}
}

// TestWriteFileCreatesParentDirectories covers the "creating parent directories
// as needed" half of the contract.
func TestWriteFileCreatesParentDirectories(t *testing.T) {
	t.Parallel()

	client, recorder := writeServer(t, 0, "")

	err := client.WriteFile(t.Context(), "/etc/kubernetes/pki/ca.key", []byte("x"), 0o600)
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	command, _ := recorder.snapshot()

	if !strings.Contains(command, "mkdir -p -- '/etc/kubernetes/pki'") {
		t.Fatalf("command does not create the parent directory: %q", command)
	}
}

// TestWriteFileReportsARemoteFailure covers the two remote refusals a caller
// must not mistake for success.
func TestWriteFileReportsARemoteFailure(t *testing.T) {
	t.Parallel()

	for name, stderr := range map[string]string{
		"missing parent":    "mkdir: cannot create directory '/etc/kubernetes/pki': Read-only file system",
		"permission denied": "chmod: changing permissions of '/etc/kubernetes/pki/ca.key': Permission denied",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client, _ := writeServer(t, 1, stderr)

			err := client.WriteFile(t.Context(), "/etc/kubernetes/pki/ca.key", []byte("x"), 0o600)
			if err == nil {
				t.Fatal("want an error, got nil")
			}

			if !errors.Is(err, sshbootstrap.ErrCommandFailed) {
				t.Fatalf("want ErrCommandFailed, got %v", err)
			}

			if errors.Is(err, sshbootstrap.ErrWriteTruncated) {
				t.Fatalf("a refusal must not be reported as truncation: %v", err)
			}
		})
	}
}

// TestWriteFileReportsATruncatedTransfer pins "never silently partial": the
// remote counts what it received and refuses to publish a short file.
func TestWriteFileReportsATruncatedTransfer(t *testing.T) {
	t.Parallel()

	client, _ := writeServer(t, sshbootstrap.WriteTruncatedExitCode, "")

	err := client.WriteFile(t.Context(), "/etc/kubernetes/pki/ca.key", []byte("xyz"), 0o600)
	if err == nil {
		t.Fatal("want an error, got nil")
	}

	if !errors.Is(err, sshbootstrap.ErrWriteTruncated) {
		t.Fatalf("want ErrWriteTruncated, got %v", err)
	}
}

// shellExecHandler answers an exec request by actually running the command in
// /bin/sh with the received stdin, so the remote half is exercised for real
// rather than through a scripted exit code.
func shellExecHandler(ctx context.Context, captured *writeRecorder) execHandler {
	return func(command string, stdin []byte) (string, string, uint32) {
		captured.record(command, stdin)

		//nolint:gosec // G204: the command is this package's own rendered script,
		// captured from the stub server; exercising it is the point of the test.
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
		cmd.Stdin = bytes.NewReader(stdin)

		var errBuf bytes.Buffer

		cmd.Stderr = &errBuf

		err := cmd.Run()
		code := 0

		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitCodeOf(exitErr)
		}

		return "", errBuf.String(), uint32(code)
	}
}

// exitCodeOf reads an exit status, mapping a signal death (-1) onto a plain
// failure so it fits the SSH exit-status request's unsigned range.
func exitCodeOf(exitErr *exec.ExitError) int {
	code := exitErr.ExitCode()
	if code < 0 {
		return 1
	}

	return code
}

// TestWriteFileScriptRunsInARealShell proves the rendered script is valid POSIX
// shell that does what the scripted-exit-code tests above only assert the shape
// of: it creates parents, applies the mode, publishes the exact bytes, and — on
// a short transfer — refuses without touching the destination it would have
// replaced. Everything runs locally against a temp directory; no network, no
// cluster, no credentials.
func TestWriteFileScriptRunsInARealShell(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("the remote half is a POSIX shell script; nodes are Linux")
	}

	dir := t.TempDir()
	dest := filepath.Join(dir, "nested", "deeper", "ca.key")
	secret := []byte(
		"-----BEGIN PRIVATE KEY-----\n" + payloadMarker + "\n-----END PRIVATE KEY-----\n",
	)

	pair := mustGenerateKeyPair(t)
	recorder := &writeRecorder{}
	addr, hostKey := startServer(
		t, pair.Signer.PublicKey(), shellExecHandler(t.Context(), recorder), 0,
	)
	client := mustDial(t, addr, pair, hostKey)

	err := client.WriteFile(t.Context(), dest, secret, 0o600)
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	assertPublished(t, dest, secret)

	// Replay the very same script with a short stdin: the destination now holds
	// known-good content, so clobbering it would be visible.
	command, _ := recorder.snapshot()
	//nolint:gosec // G204: replays the script this package just rendered.
	replay := exec.CommandContext(t.Context(), "/bin/sh", "-c", command)
	replay.Stdin = strings.NewReader("short")

	var exitErr *exec.ExitError
	if !errors.As(replay.Run(), &exitErr) ||
		exitErr.ExitCode() != sshbootstrap.WriteTruncatedExitCode {
		t.Fatalf(
			"truncated replay: want exit %d, got %v",
			sshbootstrap.WriteTruncatedExitCode,
			exitErr,
		)
	}

	assertPublished(t, dest, secret)
}

// assertPublished checks the destination holds exactly want, owner-only, with
// no staging file left beside it.
func assertPublished(t *testing.T, dest string, want []byte) {
	t.Helper()

	//nolint:gosec // G304: reads a file just written under the test's own t.TempDir().
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("destination not readable: %v", err)
	}

	if !bytes.Equal(got, want) {
		t.Fatalf("destination content: got %q, want %q", got, want)
	}

	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat destination: %v", err)
	}

	if info.Mode().Perm() != 0o600 {
		t.Fatalf("destination mode: got %04o, want 0600", info.Mode().Perm())
	}

	_, err = os.Stat(dest + ".ksail-partial")
	if !os.IsNotExist(err) {
		t.Fatal("staging file left beside the destination")
	}
}
