package eksctl

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
)

// errorOutputTailLines bounds how many trailing lines of eksctl output an error carries.
// eksctl logs the cause of a failure on stdout just before it exits, so the end of the
// output is where the diagnosis is; the rest stays out of Go error strings.
const errorOutputTailLines = 20

// maxPendingLineBytes caps how much of an unterminated line a lineWriter holds before it
// forwards the partial line anyway, so a stream without newlines cannot grow memory unbounded.
// A line longer than this is redacted chunk by chunk, so a credential value that straddles
// the boundary would not be matched; eksctl's log lines are far shorter than the cap.
const maxPendingLineBytes = 64 * 1024

// lineWriter forwards only complete lines to its target, optionally transforming each one.
// Whole-line writes keep two streams that share one target from interleaving mid-line, and
// let redaction see a credential value in one piece. Progress is best-effort: a failing
// target never fails the command whose output is being streamed.
type lineWriter struct {
	mu        sync.Mutex
	target    io.Writer
	transform func([]byte) []byte
	pending   []byte
}

// newLineWriter returns a lineWriter forwarding to target; transform may be nil.
func newLineWriter(target io.Writer, transform func([]byte) []byte) *lineWriter {
	return &lineWriter{
		mu:        sync.Mutex{},
		target:    target,
		transform: transform,
		pending:   nil,
	}
}

// Write buffers p and forwards every complete line it now holds.
func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.pending = append(w.pending, p...)

	for {
		index := bytes.IndexByte(w.pending, '\n')
		if index < 0 {
			break
		}

		w.forward(w.pending[:index+1])
		w.pending = w.pending[index+1:]
	}

	if len(w.pending) >= maxPendingLineBytes {
		w.forward(w.pending)
		w.pending = nil
	}

	return len(p), nil
}

// Flush forwards a final unterminated line, if any.
func (w *lineWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.pending) > 0 {
		w.forward(w.pending)
		w.pending = nil
	}
}

// forward writes one chunk to the target, ignoring target errors because progress is best-effort.
func (w *lineWriter) forward(chunk []byte) {
	out := append([]byte(nil), chunk...)
	if w.transform != nil {
		out = w.transform(out)
	}

	_, _ = w.target.Write(out)
}

// outputTail returns the last errorOutputTailLines non-empty lines of stdout followed by stderr.
func outputTail(stdout, stderr []byte) string {
	lines := make([]string, 0, errorOutputTailLines)

	for _, stream := range [][]byte{stdout, stderr} {
		for line := range strings.SplitSeq(string(stream), "\n") {
			if trimmed := strings.TrimRight(line, "\r "); strings.TrimSpace(trimmed) != "" {
				lines = append(lines, trimmed)
			}
		}
	}

	if len(lines) > errorOutputTailLines {
		lines = lines[len(lines)-errorOutputTailLines:]
	}

	return strings.Join(lines, "\n")
}

// withOutputTail appends the trailing eksctl output to err when it adds anything beyond the
// first stderr line the error already names.
func withOutputTail(err error, stdout, stderr []byte, firstStderrLine string) error {
	tail := outputTail(stdout, stderr)
	if tail == "" || tail == firstStderrLine {
		return err
	}

	return fmt.Errorf("%w\neksctl output (last %d lines):\n%s", err, errorOutputTailLines, tail)
}
