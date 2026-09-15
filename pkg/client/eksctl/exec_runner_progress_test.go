package eksctl_test

import (
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errProgressClosed = errors.New("progress writer closed")

// windowsGOOS is the runtime.GOOS value whose shell these tests do not target.
const windowsGOOS = "windows"

// lockedBuffer is a goroutine-safe buffer: os/exec copies stdout and stderr on separate goroutines.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	n, err := b.buf.Write(p)
	if err != nil {
		return n, fmt.Errorf("write locked buffer: %w", err)
	}

	return n, nil
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// failingWriter rejects every write, standing in for a closed terminal.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errProgressClosed
}

// TestExecRunner_RunWithProgress_StreamsBothStreamsFromARealProcess exercises the real
// os/exec path: both streams reach progress, including a final line without a newline,
// and the buffered output is still returned intact alongside the wrapped exit error.
func TestExecRunner_RunWithProgress_StreamsBothStreamsFromARealProcess(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == windowsGOOS {
		t.Skip("uses a POSIX shell")
	}

	var progress lockedBuffer

	stdout, stderr, err := eksctl.ExecRunner{}.RunWithProgress(
		t.Context(),
		"sh",
		[]string{"-c", "printf 'out-1\\nout-2'; printf 'err-1\\n' >&2; exit 3"},
		nil,
		nil,
		&progress,
	)

	require.ErrorIs(t, err, eksctl.ErrExecFailed)
	assert.Equal(t, "out-1\nout-2", string(stdout))
	assert.Equal(t, "err-1\n", string(stderr))
	assert.Contains(t, progress.String(), "out-1\n")
	assert.Contains(t, progress.String(), "out-2")
	assert.Contains(t, progress.String(), "err-1\n")
}

// TestExecRunner_RunWithProgress_FailingProgressDoesNotFailTheCommand verifies progress is
// best-effort: a writer that rejects output never turns a successful command into a failure.
func TestExecRunner_RunWithProgress_FailingProgressDoesNotFailTheCommand(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == windowsGOOS {
		t.Skip("uses a POSIX shell")
	}

	stdout, _, err := eksctl.ExecRunner{}.RunWithProgress(
		t.Context(),
		"sh",
		[]string{"-c", "printf 'created\\n'"},
		nil,
		nil,
		failingWriter{},
	)

	require.NoError(t, err)
	assert.Equal(t, "created\n", string(stdout))
}

// TestExecRunner_RunWithProgress_SerializesStreamsForAPlainWriter verifies a caller may pass a
// writer that is not safe for concurrent use: stdout and stderr lines must not race or be lost.
// Run with -race to catch an unserialized write.
func TestExecRunner_RunWithProgress_SerializesStreamsForAPlainWriter(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == windowsGOOS {
		t.Skip("uses a POSIX shell")
	}

	var progress bytes.Buffer

	_, _, err := eksctl.ExecRunner{}.RunWithProgress(
		t.Context(),
		"sh",
		[]string{"-c", "i=0; while [ $i -lt 200 ]; do echo out-$i; echo err-$i >&2; i=$((i+1)); done"},
		nil,
		nil,
		&progress,
	)

	require.NoError(t, err)
	assert.Equal(t, 200, bytes.Count(progress.Bytes(), []byte("out-")))
	assert.Equal(t, 200, bytes.Count(progress.Bytes(), []byte("err-")))
}
