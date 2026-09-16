package eksctl

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"unicode/utf8"
)

// errorOutputStdoutLines bounds how many trailing stdout lines an error carries. eksctl logs the
// cause of a failure on stdout just before it exits, so the end of stdout is where the diagnosis is.
const errorOutputStdoutLines = 15

// errorOutputStderrLines bounds how many trailing stderr lines an error carries. It is capped
// separately from stdout so a long stderr can never push the stdout cause out of the error.
const errorOutputStderrLines = 5

// maxErrorTailLineBytes caps how many bytes of any single line the failure tail keeps. tailLines
// bounds the tail by line COUNT, which is not a size bound on its own: eksctl can emit one very
// long line (a CloudFormation reason, or a JSON payload), so without this the tail could carry
// the whole of it. With it, the tail is bounded by construction at
// (errorOutputStdoutLines + errorOutputStderrLines) * maxErrorTailLineBytes.
const maxErrorTailLineBytes = 512

// errorTailLineTruncationMarker ends a line the failure tail shortened, so a truncated cause is
// never mistaken for the whole of it.
const errorTailLineTruncationMarker = " …[truncated]"

// maxPendingLineBytes caps how much of an unterminated line a lineWriter holds. A longer line is
// dropped from the stream and replaced by a placeholder rather than forwarded in pieces: redaction
// sees one piece at a time, so a credential split across two pieces would otherwise leak.
const maxPendingLineBytes = 64 * 1024

// omittedLinePlaceholder replaces a line longer than maxPendingLineBytes in the forwarded stream.
const omittedLinePlaceholder = "[line longer than 64 KiB omitted]\n"

// lineWriter forwards only complete lines to its target, optionally transforming each one.
// Whole-line writes keep two streams that share one target from interleaving mid-line, and let
// redaction see a credential value in one piece. Progress is best-effort: a failing target never
// fails the command whose output is being streamed.
type lineWriter struct {
	mu        sync.Mutex
	target    io.Writer
	transform func([]byte) []byte
	pending   []byte
	overlong  bool
}

// newLineWriter returns a lineWriter forwarding to target; transform may be nil.
func newLineWriter(target io.Writer, transform func([]byte) []byte) *lineWriter {
	return &lineWriter{
		mu:        sync.Mutex{},
		target:    target,
		transform: transform,
		pending:   nil,
		overlong:  false,
	}
}

// Write buffers data and forwards every line it completes.
func (w *lineWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	written := len(data)

	for len(data) > 0 {
		index := bytes.IndexByte(data, '\n')
		if index < 0 {
			w.hold(data)

			break
		}

		w.hold(data[:index+1])
		w.finishLine()

		data = data[index+1:]
	}

	return written, nil
}

// Flush forwards a final unterminated line, or its placeholder if it outgrew the cap.
func (w *lineWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.overlong || len(w.pending) > 0 {
		w.finishLine()
	}
}

// hold appends part of the current line, or drops the line once it outgrows the cap.
func (w *lineWriter) hold(part []byte) {
	if w.overlong {
		return
	}

	if len(w.pending)+len(part) > maxPendingLineBytes {
		w.pending = nil
		w.overlong = true

		return
	}

	w.pending = append(w.pending, part...)
}

// finishLine forwards the held line, or a placeholder for a line dropped as overlong.
func (w *lineWriter) finishLine() {
	if w.overlong {
		w.forward([]byte(omittedLinePlaceholder))
	} else {
		w.forward(w.pending)
	}

	w.pending = nil
	w.overlong = false
}

// forward writes one line to the target, ignoring target errors because progress is best-effort.
func (w *lineWriter) forward(line []byte) {
	out := append([]byte(nil), line...)
	if w.transform != nil {
		out = w.transform(out)
	}

	_, _ = w.target.Write(out)
}

// lockedWriter serializes writes to a target that may not be safe for concurrent use.
type lockedWriter struct {
	mu     sync.Mutex
	target io.Writer
}

// newLockedWriter returns a writer that serializes every write to target.
func newLockedWriter(target io.Writer) *lockedWriter {
	return &lockedWriter{mu: sync.Mutex{}, target: target}
}

// Write writes data to the target while holding the lock.
func (w *lockedWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	written, err := w.target.Write(data)
	if err != nil {
		return written, fmt.Errorf("write eksctl progress: %w", err)
	}

	return written, nil
}

// outputTail returns the last non-empty stdout lines followed by the last non-empty stderr lines,
// each stream bounded on its own.
func outputTail(stdout, stderr []byte) string {
	stdoutLines := tailLines(stdout, errorOutputStdoutLines)
	stderrLines := tailLines(stderr, errorOutputStderrLines)

	lines := make([]string, 0, len(stdoutLines)+len(stderrLines))
	lines = append(lines, stdoutLines...)
	lines = append(lines, stderrLines...)

	return strings.Join(lines, "\n")
}

// tailLines returns the last limit non-empty lines of stream, with trailing spaces trimmed and
// each line capped at maxErrorTailLineBytes, so the tail is bounded in bytes and not only in lines.
func tailLines(stream []byte, limit int) []string {
	lines := make([]string, 0, limit)

	for line := range strings.SplitSeq(string(stream), "\n") {
		if trimmed := strings.TrimRight(line, "\r "); strings.TrimSpace(trimmed) != "" {
			lines = append(lines, truncateTailLine(trimmed))
		}
	}

	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}

	return lines
}

// truncateTailLine caps one tail line at maxErrorTailLineBytes. It cuts on a rune boundary so a
// multi-byte character is never split, and marks the line so a shortened cause cannot be read as
// the whole of it. The streams reaching here are already redacted, so truncating cannot expose
// part of a credential value.
func truncateTailLine(line string) string {
	if len(line) <= maxErrorTailLineBytes {
		return line
	}

	cut := maxErrorTailLineBytes
	for cut > 0 && !utf8.RuneStart(line[cut]) {
		cut--
	}

	return line[:cut] + errorTailLineTruncationMarker
}

// withOutputTail appends the trailing eksctl output to err when it adds anything beyond the
// first stderr line the error already names.
func withOutputTail(err error, stdout, stderr []byte, firstStderrLine string) error {
	tail := outputTail(stdout, stderr)
	if tail == "" || tail == firstStderrLine {
		return err
	}

	return fmt.Errorf(
		"%w\neksctl output (last %d stdout and %d stderr lines):\n%s",
		err,
		errorOutputStdoutLines,
		errorOutputStderrLines,
		tail,
	)
}
