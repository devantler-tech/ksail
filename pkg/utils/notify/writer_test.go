package notify_test

import (
	"bytes"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeferredNewlineWriter_SingleWrite(t *testing.T) {
	t.Parallel()

	writer := notify.NewDeferredNewlineWriter(nil)

	_, err := writer.Write([]byte("hello\n"))
	require.NoError(t, err)

	// Trailing newline should be held
	assert.Equal(t, "hello", writer.String())
}

func TestDeferredNewlineWriter_MultipleWrites(t *testing.T) {
	t.Parallel()

	writer := notify.NewDeferredNewlineWriter(nil)

	_, err := writer.Write([]byte("line1\n"))
	require.NoError(t, err)

	_, err = writer.Write([]byte("line2\n"))
	require.NoError(t, err)

	_, err = writer.Write([]byte("line3\n"))
	require.NoError(t, err)

	// Pending newline from each write should be flushed before next write
	// Final newline should be held
	assert.Equal(t, "line1\nline2\nline3", writer.String())
}

func TestDeferredNewlineWriter_NoTrailingNewline(t *testing.T) {
	t.Parallel()

	writer := notify.NewDeferredNewlineWriter(nil)

	_, err := writer.Write([]byte("no newline"))
	require.NoError(t, err)

	assert.Equal(t, "no newline", writer.String())
}

func TestDeferredNewlineWriter_Flush(t *testing.T) {
	t.Parallel()

	writer := notify.NewDeferredNewlineWriter(nil)

	_, err := writer.Write([]byte("hello\n"))
	require.NoError(t, err)

	// Before flush: newline is held
	assert.Equal(t, "hello", writer.String())

	// After flush: newline is written
	err = writer.Flush()
	require.NoError(t, err)
	assert.Equal(t, "hello\n", writer.String())
}

func TestDeferredNewlineWriter_Reset(t *testing.T) {
	t.Parallel()

	writer := notify.NewDeferredNewlineWriter(nil)

	_, err := writer.Write([]byte("content\n"))
	require.NoError(t, err)

	writer.Reset()
	assert.Empty(t, writer.String())
}

func TestDeferredNewlineWriter_WithUnderlying(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	writer := notify.NewDeferredNewlineWriter(&buf)

	_, err := writer.Write([]byte("hello\n"))
	require.NoError(t, err)

	// Content without trailing newline written to underlying
	assert.Equal(t, "hello", buf.String())

	// String() returns empty when using underlying writer
	assert.Empty(t, writer.String())

	// Flush writes the pending newline
	err = writer.Flush()
	require.NoError(t, err)
	assert.Equal(t, "hello\n", buf.String())
}

func TestDeferredNewlineWriter_EmptyWrite(t *testing.T) {
	t.Parallel()

	writer := notify.NewDeferredNewlineWriter(nil)

	n, err := writer.Write([]byte{})
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Empty(t, writer.String())
}

func TestDeferredNewlineWriter_OnlyNewline(t *testing.T) {
	t.Parallel()

	writer := notify.NewDeferredNewlineWriter(nil)

	n, err := writer.Write([]byte("\n"))
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// Newline is pending, buffer is empty
	assert.Empty(t, writer.String())

	// Next write flushes the pending newline
	_, err = writer.Write([]byte("after\n"))
	require.NoError(t, err)
	assert.Equal(t, "\nafter", writer.String())
}

func TestDeferredNewlineWriter_MultipleNewlines(t *testing.T) {
	t.Parallel()

	writer := notify.NewDeferredNewlineWriter(nil)

	_, err := writer.Write([]byte("line\n\n"))
	require.NoError(t, err)

	// Should preserve internal newline but hold final one
	// The write includes "line\n\n" - content is "line\n", trailing is "\n"
	assert.Equal(t, "line\n", writer.String())
}

func TestDeferredNewlineWriter_ByteCount(t *testing.T) {
	t.Parallel()

	writer := notify.NewDeferredNewlineWriter(nil)

	n, err := writer.Write([]byte("hello\n"))
	require.NoError(t, err)
	assert.Equal(t, 6, n) // Reports full length including held newline
}
