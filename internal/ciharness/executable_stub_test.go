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
	child := exec.CommandContext(t.Context(), "sh", "-c", `read -r line && exec "$1"`, "sh", stubPath)
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

	return stderr.String(), child.Wait()
}
