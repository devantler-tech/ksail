package notify

import (
	"bytes"
	"fmt"
	"io"
	"sync"
)

// DeferredNewlineWriter wraps an io.Writer and defers trailing newlines.
// Instead of immediately writing a trailing newline, it holds it until the next write.
// This allows the final newline to be discarded when Flush() is called or when the
// writer is finalized, resulting in clean output without trailing blank lines.
//
// Usage:
//
//	var buf bytes.Buffer
//	w := notify.NewDeferredNewlineWriter(&buf)
//	// ... use w for all notify.WriteMessage calls ...
//	output := w.String() // Returns content without trailing newline
type DeferredNewlineWriter struct {
	underlying     io.Writer
	buf            *bytes.Buffer // Used when underlying is nil (standalone buffer mode)
	pendingNewline bool
	mu             sync.Mutex
}

// NewDeferredNewlineWriter creates a new DeferredNewlineWriter.
// If underlying is nil, it creates an internal buffer that can be
// retrieved via String().
func NewDeferredNewlineWriter(underlying io.Writer) *DeferredNewlineWriter {
	var buf *bytes.Buffer
	if underlying == nil {
		buf = &bytes.Buffer{}
		underlying = buf
	}

	return &DeferredNewlineWriter{
		underlying: underlying,
		buf:        buf,
	}
}

// Write implements io.Writer.
// If there's a pending newline from a previous write, it writes that first.
// Then it writes the content, but holds any trailing newline.
func (w *DeferredNewlineWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(data) == 0 {
		return 0, nil
	}

	// If we have a pending newline, write it before the new content
	if w.pendingNewline {
		_, err := w.underlying.Write([]byte{'\n'})
		if err != nil {
			return 0, fmt.Errorf("write pending newline: %w", err)
		}

		w.pendingNewline = false
	}

	// Check if the content ends with a newline
	endsWithNewline := data[len(data)-1] == '\n'

	// Write content, excluding trailing newline if present
	contentToWrite := data
	if endsWithNewline {
		contentToWrite = data[:len(data)-1]
	}

	written := 0

	var writeErr error

	if len(contentToWrite) > 0 {
		written, writeErr = w.underlying.Write(contentToWrite)
		if writeErr != nil {
			return written, fmt.Errorf("write content: %w", writeErr)
		}
	}

	// Hold the newline for later
	if endsWithNewline {
		w.pendingNewline = true
		// Report full length including the newline we're holding
		return len(data), nil
	}

	return written, nil
}

// String returns the buffered content without the pending trailing newline.
// Only works if the writer was created with a nil underlying writer (standalone buffer mode).
// Returns empty string if not in buffer mode.
func (w *DeferredNewlineWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.buf == nil {
		return ""
	}

	return w.buf.String()
}

// Flush writes any pending newline to the underlying writer.
// Call this if you want to preserve the trailing newline (e.g., for intermediate output).
func (w *DeferredNewlineWriter) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.pendingNewline {
		_, err := w.underlying.Write([]byte{'\n'})
		if err != nil {
			return fmt.Errorf("flush pending newline: %w", err)
		}

		w.pendingNewline = false
	}

	return nil
}

// Reset clears the buffer and pending state.
// Only applicable in buffer mode.
func (w *DeferredNewlineWriter) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.buf != nil {
		w.buf.Reset()
	}

	w.pendingNewline = false
}
