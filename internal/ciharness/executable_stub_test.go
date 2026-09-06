package ciharness_test

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWriteExecutableStub_ProducesRunnableOwnerOnlyStub pins the writer's
// contract: the stub carries exactly the given content, is executable by its
// owner only, and can be executed immediately after the call returns.
func TestWriteExecutableStub_ProducesRunnableOwnerOnlyStub(t *testing.T) {
	t.Parallel()

	stubPath := filepath.Join(t.TempDir(), "stub")
	content := "#!/bin/sh\nprintf 'stub ran %s\\n' \"$1\"\n"

	writeExecutableStub(t, stubPath, content)

	written, err := os.ReadFile(stubPath) //nolint:gosec // Test-owned temp path.
	require.NoError(t, err)
	assert.Equal(t, content, string(written), "stub content must be written verbatim")

	info, err := os.Stat(stubPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm(), "stub must be owner-only executable")

	//nolint:gosec // stubPath is a test-owned temp file the writer under test just created.
	output, err := exec.CommandContext(t.Context(), stubPath, "now").CombinedOutput()
	require.NoError(t, err, "stub must be executable right after it is written: %s", output)
	assert.Equal(t, "stub ran now\n", string(output))
}

// TestWriteExecutableStub_ExecutesUnderConcurrentForks is a load smoke for #6199:
// many goroutines write a stub and execute it at once, so every other goroutine's
// fork is a candidate to inherit a still-open write descriptor. It samples the
// race rather than forcing it — the deterministic reproduction is
// TestWriteExecutableStub_InheritedWriteDescriptorBlocksExec — so it must simply
// never fail, however the scheduler interleaves the workers.
func TestWriteExecutableStub_ExecutesUnderConcurrentForks(t *testing.T) {
	t.Parallel()

	const workers = 24

	dir := t.TempDir()

	var waitGroup sync.WaitGroup

	errs := make(chan error, workers)

	for worker := range workers {
		waitGroup.Go(func() {
			stubPath := filepath.Join(dir, "stub-"+strconv.Itoa(worker))

			err := writeExecutableFile(t.Context(), stubPath, "#!/bin/sh\nexit 0\n")
			if err != nil {
				errs <- err

				return
			}

			//nolint:gosec // stubPath is a test-owned temp file the writer under test just created.
			errs <- exec.CommandContext(t.Context(), stubPath).Run()
		})
	}

	waitGroup.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err, "a stub written under concurrent forks must execute cleanly")
	}
}

// TestWriteExecutableStub_InheritedWriteDescriptorBlocksExec is the coordinated
// regression behind #6199. It forces the race instead of sampling it: this
// process holds a stub open for writing while it starts a child that inherits
// that descriptor (ExtraFiles survives exec, unlike Go's default close-on-exec
// descriptors), then closes its own copy and only then lets the child execute
// the stub. The child still holds the inherited descriptor, so the kernel refuses
// the exec with ETXTBSY — the exact "bad interpreter: Text file busy" the
// harness hit. A stub written by writeExecutableFile leaves this process with no
// descriptor to inherit, so the same executor runs it cleanly.
func TestWriteExecutableStub_InheritedWriteDescriptorBlocksExec(t *testing.T) {
	t.Parallel()

	// Linux refuses to execute a file that any process holds open for writing;
	// macOS does not (verified: the control below succeeds there), and the CI
	// runners that hit #6199 are Linux.
	if runtime.GOOS != "linux" {
		t.Skip("ETXTBSY on exec of a file open for writing is Linux semantics")
	}

	dir := t.TempDir()
	content := "#!/bin/sh\nexit 0\n"

	// The hazard: a writer this process opened, inherited by the child that execs.
	heldPath := filepath.Join(dir, "held-open")
	require.NoError(t, os.WriteFile(heldPath, []byte(content), 0o600))
	//nolint:gosec // Owner execute is required for a PATH stub in a private temp dir.
	require.NoError(t, os.Chmod(heldPath, 0o700))

	writer, err := os.OpenFile(heldPath, os.O_WRONLY, 0) //nolint:gosec // Test-owned temp path.
	require.NoError(t, err)

	stderr, err := execAfterSignal(t, heldPath, writer)
	require.Error(t, err, "a child holding an inherited write descriptor must not execute the stub")
	assert.Contains(t, strings.ToLower(stderr), "text file busy")

	// The fix: the helper never gives this process a descriptor a child could inherit.
	safePath := filepath.Join(dir, "helper-written")
	require.NoError(t, writeExecutableFile(t.Context(), safePath, content))

	stderr, err = execAfterSignal(t, safePath, nil)
	require.NoError(t, err, "helper-written stub must execute: %s", stderr)
}

// execAfterSignal starts a child that waits for a line on stdin before exec'ing
// stubPath, so the caller controls exactly when the exec happens. When inherit is
// non-nil the child receives it as an extra descriptor (ExtraFiles survives exec,
// unlike Go's default close-on-exec descriptors) and this process closes its own
// copy before releasing the child, leaving the child's inherited descriptor as the
// only writer open on the stub. It returns the child's stderr and its wait error.
func execAfterSignal(t *testing.T, stubPath string, inherit *os.File) (string, error) {
	t.Helper()

	var stderr bytes.Buffer

	//nolint:gosec // stubPath is a test-owned temp file; the shell fragment is a constant.
	child := exec.CommandContext(
		t.Context(),
		"sh",
		"-c",
		`read -r line && exec "$1"`,
		"sh",
		stubPath,
	)
	child.Stderr = &stderr

	if inherit != nil {
		child.ExtraFiles = []*os.File{inherit}
	}

	stdin, err := child.StdinPipe()
	require.NoError(t, err)
	require.NoError(t, child.Start())

	if inherit != nil {
		require.NoError(t, inherit.Close())
	}

	_, err = io.WriteString(stdin, "go\n")
	require.NoError(t, err)
	require.NoError(t, stdin.Close())

	// Wait first: the child is still writing to stderr until it exits, so reading
	// the buffer in the same return statement would race the exec failure message
	// (Go evaluates the operands left to right).
	waitErr := child.Wait()

	return stderr.String(), waitErr
}

// TestExecAfterSignal_CapturesStderrAfterExit pins the helper's contract on any
// platform: the child is still writing while it runs, so the returned stderr
// must be read after it exits. Reading it in the same return statement as
// Wait() captures an empty buffer, which silently turns the ETXTBSY assertion
// above into a vacuous one.
func TestExecAfterSignal_CapturesStderrAfterExit(t *testing.T) {
	t.Parallel()

	stubPath := filepath.Join(t.TempDir(), "late-writer")
	require.NoError(t, writeExecutableFile(
		t.Context(), stubPath,
		"#!/bin/sh\nsleep 0.2\necho 'stub failed late' >&2\nexit 3\n",
	))

	stderr, err := execAfterSignal(t, stubPath, nil)

	require.Error(t, err, "the stub exits non-zero")
	assert.Contains(t, stderr, "stub failed late",
		"stderr must be read after the child exits, not while it is still running")
}

// TestWriteExecutableStub_ConcurrentWriteBlocksExec is the regression that
// separates the two writers, and the reason writeExecutableFile exists.
//
// The kernel refuses to exec a file while any process holds it open for writing.
// An in-process writer therefore poisons every concurrent exec of that stub for
// as long as it is open — and the harness's tests fork constantly, which is how
// #6199 surfaced. The control below makes that window explicit: a goroutine holds
// the descriptor while this test execs the same path, and the exec fails.
// writeExecutableFile cannot reproduce it, because the only descriptor lives in a
// child shell that has exited by the time the call returns, so the same exec of
// the same path succeeds.
func TestWriteExecutableStub_ConcurrentWriteBlocksExec(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "linux" {
		t.Skip("ETXTBSY on exec of a file open for writing is Linux semantics")
	}

	dir := t.TempDir()
	content := "#!/bin/sh\nexit 0\n"

	// Control: this process holds the write descriptor across the exec.
	inProcess := filepath.Join(dir, "in-process-writer")
	require.NoError(t, os.WriteFile(inProcess, []byte(content), 0o600))
	//nolint:gosec // Owner execute is required for a PATH stub in a private temp dir.
	require.NoError(t, os.Chmod(inProcess, 0o700))

	// The writer goroutine reports through channels rather than asserting:
	// testify's require must only run on the test goroutine.
	execDone := make(chan struct{})
	openErr := make(chan error, 1)
	closeErr := make(chan error, 1)

	go func() {
		handle, err := os.OpenFile(
			inProcess,
			os.O_WRONLY,
			0,
		) //nolint:gosec // Test-owned temp path.

		openErr <- err

		if err != nil {
			closeErr <- nil

			return
		}

		<-execDone

		closeErr <- handle.Close()
	}()

	require.NoError(t, <-openErr, "could not hold the stub open for writing")

	//nolint:gosec // inProcess is a test-owned temp file this test just created.
	output, err := exec.CommandContext(t.Context(), inProcess).CombinedOutput()

	close(execDone)
	require.NoError(t, <-closeErr)

	require.Error(t, err, "exec must fail while a writer holds the stub open: %s", output)

	// The fix: writeExecutableFile leaves no writer anywhere once it returns.
	viaHelper := filepath.Join(dir, "helper-writer")
	require.NoError(t, writeExecutableFile(t.Context(), viaHelper, content))

	//nolint:gosec // viaHelper is a test-owned temp file the writer under test just created.
	output, err = exec.CommandContext(t.Context(), viaHelper).CombinedOutput()
	require.NoError(t, err, "helper-written stub must exec cleanly: %s", output)
}
