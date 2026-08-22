package sshbootstrap_test

import (
	"bytes"
	"context"
	"errors"
	"io"
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

// osWindows names the one platform whose shell cannot run the remote half.
const osWindows = "windows"

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

// record stores what one exec request carried. stdin may be nil when the
// handler streamed the input straight through instead of draining it.
func (r *writeRecorder) record(command string, stdin []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.command = command

	r.stdin = append([]byte(nil), stdin...)
}

// snapshot returns a copy of the recorded command and stdin, safe to read
// while the server goroutine is still running.
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
		func(command string, stdin io.Reader) (string, string, uint32) {
			drained, _ := io.ReadAll(stdin)
			recorder.record(command, drained)

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
	return func(command string, stdin io.Reader) (string, string, uint32) {
		// Record the command only: stdin is streamed straight to the shell, so
		// draining it here would defeat the point of handing over a live reader.
		captured.record(command, nil)

		//nolint:gosec // G204: the command is this package's own rendered script,
		// captured from the stub server; exercising it is the point of the test.
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
		cmd.Stdin = stdin

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

	if runtime.GOOS == osWindows {
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

// TestWriteFileDoesNotFollowAPrePlacedStagingSymlink pins a real attack the
// staging step would otherwise enable: the staging name is derived from the
// destination and therefore predictable, so anything already sitting there is
// attacker-chosen. A shell redirect follows a symlink and writes through to its
// target, which would deliver the secret to a file of someone else's choosing.
func TestWriteFileDoesNotFollowAPrePlacedStagingSymlink(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == osWindows {
		t.Skip("the remote half is a POSIX shell script; nodes are Linux")
	}

	dir := t.TempDir()
	dest := filepath.Join(dir, "ca.key")
	victim := filepath.Join(dir, "victim")
	untouched := []byte("VICTIM-CONTENT\n")

	err := os.WriteFile(victim, untouched, 0o600)
	if err != nil {
		t.Fatalf("seed victim: %v", err)
	}

	// Someone gets there first and points the staging name at the victim.
	err = os.Symlink(victim, dest+".ksail-partial")
	if err != nil {
		t.Fatalf("pre-place symlink: %v", err)
	}

	secret := []byte(
		"-----BEGIN PRIVATE KEY-----\n" + payloadMarker + "\n-----END PRIVATE KEY-----\n",
	)

	pair := mustGenerateKeyPair(t)
	addr, hostKey := startServer(
		t, pair.Signer.PublicKey(), shellExecHandler(t.Context(), &writeRecorder{}), 0,
	)
	client := mustDial(t, addr, pair, hostKey)

	err = client.WriteFile(t.Context(), dest, secret, 0o600)
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	//nolint:gosec // G304: reads a file just written under the test's own t.TempDir().
	got, err := os.ReadFile(victim)
	if err != nil {
		t.Fatalf("read victim: %v", err)
	}

	if !bytes.Equal(got, untouched) {
		t.Fatalf("secret was written through the symlink into %s: %q", victim, got)
	}

	assertPublished(t, dest, secret)
}

// TestWriteFileRejectsARelativePath covers the guard that keeps the remote
// argument unambiguous: a relative path would resolve against whatever
// directory the login shell starts in.
func TestWriteFileRejectsARelativePath(t *testing.T) {
	t.Parallel()

	client, recorder := writeServer(t, 0, "")

	err := client.WriteFile(t.Context(), "etc/kubernetes/pki/ca.key", []byte("x"), 0o600)
	if !errors.Is(err, sshbootstrap.ErrRelativeRemotePath) {
		t.Fatalf("want ErrRelativeRemotePath, got %v", err)
	}

	command, _ := recorder.snapshot()
	if command != "" {
		t.Fatalf("a rejected path must not reach the node: %q", command)
	}
}

// TestWriteFileReportsAFailureThatPreemptsTheStream covers the case the scripted
// stub cannot reach: the remote aborts at mkdir and never drains stdin, while
// the client is still streaming a payload larger than the SSH window. The exit
// status must still win over the interrupted copy, and the failure must not be
// dressed up as truncation — the bytes never got as far as being counted.
func TestWriteFileReportsAFailureThatPreemptsTheStream(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == osWindows {
		t.Skip("the remote half is a POSIX shell script; nodes are Linux")
	}

	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")

	seed := []byte("a file, not a directory\n")

	err := os.WriteFile(blocker, seed, 0o600)
	if err != nil {
		t.Fatalf("seed blocker: %v", err)
	}

	// The parent of dest is a regular file, so `mkdir -p` fails immediately and
	// `set -e` aborts the script before `cat` ever reads the stream.
	dest := filepath.Join(blocker, "sub", "ca.key")
	payload := bytes.Repeat([]byte("A"), 4<<20)

	pair := mustGenerateKeyPair(t)
	addr, hostKey := startServer(
		t, pair.Signer.PublicKey(), shellExecHandler(t.Context(), &writeRecorder{}), 0,
	)
	client := mustDial(t, addr, pair, hostKey)

	err = client.WriteFile(t.Context(), dest, payload, 0o600)
	if !errors.Is(err, sshbootstrap.ErrCommandFailed) {
		t.Fatalf("want ErrCommandFailed, got %v", err)
	}

	if errors.Is(err, sshbootstrap.ErrWriteTruncated) {
		t.Fatalf("a refusal before the stream was read is not truncation: %v", err)
	}

	// dest's parent is a regular file, so nothing could have been created under
	// it — assert the blocker itself came through untouched.
	//nolint:gosec // G304: reads a file just written under the test's own t.TempDir().
	after, err := os.ReadFile(blocker)
	if err != nil || !bytes.Equal(after, seed) {
		t.Fatalf("blocker changed: %q err=%v", after, err)
	}
}

// TestWriteFileFailsClosedIfTheStagingPathIsRecreated covers the window the
// unlink alone cannot close: the unlink and the redirect are two separate
// syscalls, so an attacker who re-creates the staging path in between would be
// followed by a plain redirect. Simulated deterministically by neutralising the
// unlink in the rendered script and leaving a symlink in place, which is
// exactly the state the losing side of that race produces.
func TestWriteFileFailsClosedIfTheStagingPathIsRecreated(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == osWindows {
		t.Skip("the remote half is a POSIX shell script; nodes are Linux")
	}

	dir := t.TempDir()
	dest := filepath.Join(dir, "ca.key")
	victim := filepath.Join(dir, "victim")
	untouched := []byte("VICTIM-CONTENT\n")

	err := os.WriteFile(victim, untouched, 0o600)
	if err != nil {
		t.Fatalf("seed victim: %v", err)
	}

	// Capture the script the client would send.
	client, recorder := writeServer(t, 0, "")
	secret := []byte(
		"-----BEGIN PRIVATE KEY-----\n" + payloadMarker + "\n-----END PRIVATE KEY-----\n",
	)

	err = client.WriteFile(t.Context(), dest, secret, 0o600)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}

	script, _ := recorder.snapshot()

	if !strings.Contains(script, "set -C") {
		t.Fatal("script does not enable noclobber, so the race window is open")
	}

	// Drop the unlink: this is the attacker winning the race.
	raced := strings.Replace(script, "rm -f -- '"+dest+".ksail-partial'", "true", 1)
	if raced == script {
		t.Fatal("could not neutralise the unlink; the simulation would be vacuous")
	}

	err = os.Symlink(victim, dest+".ksail-partial")
	if err != nil {
		t.Fatalf("plant symlink: %v", err)
	}

	//nolint:gosec // G204: replays this package's own rendered script.
	replay := exec.CommandContext(t.Context(), "/bin/sh", "-c", raced)
	replay.Stdin = bytes.NewReader(secret)

	if replay.Run() == nil {
		t.Fatal("the write succeeded through a re-created staging path")
	}

	//nolint:gosec // G304: reads a file just written under the test's own t.TempDir().
	after, err := os.ReadFile(victim)
	if err != nil || !bytes.Equal(after, untouched) {
		t.Fatalf("secret reached the victim through the race: %q err=%v", after, err)
	}
}
