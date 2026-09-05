package ciharness_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

	output, err := exec.CommandContext(t.Context(), stubPath, "now").CombinedOutput()
	require.NoError(t, err, "stub must be executable right after it is written: %s", output)
	assert.Equal(t, "stub ran now\n", string(output))
}

// TestWriteExecutableStub_ExecutesUnderConcurrentForks is the regression
// signal for #6199: many goroutines write a stub and execute it at once, while
// every other goroutine's fork is a candidate to inherit a still-open write
// descriptor. With the descriptor confined to a child shell, no exec can observe
// the stub open for writing, so this must pass without a single ETXTBSY.
func TestWriteExecutableStub_ExecutesUnderConcurrentForks(t *testing.T) {
	t.Parallel()

	const workers = 24

	dir := t.TempDir()

	var waitGroup sync.WaitGroup

	errs := make(chan error, workers)

	for worker := range workers {
		waitGroup.Add(1)

		go func() {
			defer waitGroup.Done()

			stubPath := filepath.Join(dir, "stub-"+strconv.Itoa(worker))

			err := writeExecutableFile(t.Context(), stubPath, "#!/bin/sh\nexit 0\n")
			if err != nil {
				errs <- err

				return
			}

			errs <- exec.CommandContext(t.Context(), stubPath).Run()
		}()
	}

	waitGroup.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err, "a stub written under concurrent forks must execute cleanly")
	}
}
