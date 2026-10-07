package addressmask

import (
	"io"
	"strings"
	"sync"
	"time"
)

const (
	// heldTailFlushDelay is how long the end of a partial line waits for the
	// write that completes it. It is short enough that a progress indicator
	// still appears at once to a person, and long enough to join the two halves
	// of one copy that a buffer boundary split.
	heldTailFlushDelay = 50 * time.Millisecond

	// maxHeldTail bounds the held text. A longer run of address characters is
	// an encoded blob, not the start of an address, so it is masked as it
	// stands and nothing of it is held.
	maxHeldTail = 4096

	// longestAddressText is more than the longest textual IP literal (45 bytes).
	longestAddressText = 64
)

// IsMasking reports whether writer is one returned by Masker.Writer.
func IsMasking(writer io.Writer) bool {
	_, ok := writer.(*maskingWriter)

	return ok
}

// maskingWriter masks what is written through it.
//
// Text is passed on as soon as it is written, except for a trailing run of
// characters an address can consist of (hexadecimal digits, dots and colons):
// that run may be the first half of an address whose second half arrives in the
// next write, so it is held back and masked together with it. A line that ends
// in a newline, or in any other character, holds nothing back. A held run that
// no further write completes is passed on after heldTailFlushDelay, so a
// partial line is delayed by at most that long and is never dropped.
//
// Two halves of an address written further apart than heldTailFlushDelay are
// therefore not joined.
type maskingWriter struct {
	masker *Masker
	writer io.Writer

	mu         sync.Mutex
	held       string
	before     string
	timer      *time.Timer
	generation uint64
	flushAfter time.Duration
}

// Write masks the payload and reports the caller's byte count on success, as
// io.Writer requires even though the masked payload may differ in length.
func (w *maskingWriter) Write(payload []byte) (int, error) {
	if w.masker == nil || !w.masker.enabled {
		_, err := w.writer.Write(payload)
		if err != nil {
			return 0, err //nolint:wrapcheck // a transparent writer returns the sink's error as is
		}

		return len(payload), nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// The pending flush is cancelled before anything is written: a slow sink
	// must not let it run against text this write is about to hold.
	w.cancelFlush()

	text := w.held + string(payload)
	cut := heldTailStart(text)
	w.held = text[cut:]

	err := w.emit(text[:cut])

	// The wait for the completing write starts once this one is through the
	// sink, however long the sink took.
	w.scheduleFlush()

	if err != nil {
		return 0, err
	}

	return len(payload), nil
}

// emit masks text and passes it on. What was written just before it — the last
// run of address characters and the byte in front of that run — is put back in
// front for the masking only, so a literal is judged whole even when its first
// part was already passed on. The caller holds the lock.
func (w *maskingWriter) emit(text string) error {
	if text == "" {
		return nil
	}

	seen := w.before + text
	masked := w.masker.maskFrom(seen, len(w.before))
	w.before = seen[contextStart(seen):]

	_, err := io.WriteString(w.writer, masked)

	return err //nolint:wrapcheck // a transparent writer returns the sink's error as is
}

// contextStart returns where the part of printed text begins that a later write
// can still belong to: its trailing run of address characters, no longer than
// an address, and the one byte before that run.
func contextStart(text string) int {
	start := len(text)

	for start > 0 && len(text)-start < longestAddressText && isAddressByte(text[start-1]) {
		start--
	}

	return max(start-1, 0)
}

// cancelFlush withdraws the pending flush. Stopping the timer is not enough: a
// timer that already fired is waiting for the lock, so it is also outdated by
// number. The caller holds the lock.
func (w *maskingWriter) cancelFlush() {
	w.generation++

	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
}

// scheduleFlush arranges for held text to be passed on if no write completes
// it. The caller holds the lock and has cancelled the previous flush.
func (w *maskingWriter) scheduleFlush() {
	if w.held == "" {
		return
	}

	generation := w.generation

	w.timer = time.AfterFunc(w.flushAfter, func() {
		w.mu.Lock()
		defer w.mu.Unlock()

		if w.generation == generation {
			w.flushHeld()
		}
	})
}

// Flush passes on whatever is held, at once. Call it when the output is
// complete, so the end of a last partial line is not left to the timer.
func (w *maskingWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.cancelFlush()
	w.flushHeld()
}

// flushHeld passes on whatever is held. The sink's error has no caller to go
// to; the write that follows reports it again if the sink is broken. The caller
// holds the lock.
func (w *maskingWriter) flushHeld() {
	held := w.held
	w.held = ""

	_ = w.emit(held)
}

// heldTailStart returns where the trailing run of address characters begins.
func heldTailStart(text string) int {
	start := len(text)

	for start > 0 && isAddressByte(text[start-1]) {
		start--
	}

	if len(text)-start > maxHeldTail {
		return len(text)
	}

	// No address holds two dots in a row, so nothing up to the last such pair
	// can belong to one that is still arriving: "Waiting..." holds nothing.
	if dots := strings.LastIndex(text[start:], ".."); dots >= 0 {
		start += dots + len("..")
	}

	// Letters alone ("created", "added") are the end of a word far more often
	// than the first half of an address group, so they are not held.
	if !strings.ContainsAny(text[start:], "0123456789:.") {
		return len(text)
	}

	return start
}

func isAddressByte(char byte) bool {
	return char == '.' || char == ':' ||
		(char >= '0' && char <= '9') ||
		(char >= 'a' && char <= 'f') ||
		(char >= 'A' && char <= 'F')
}

// Flush passes on, at once, whatever a writer returned by Masker.Writer still
// holds. It does nothing for any other writer.
func Flush(writer io.Writer) {
	if masked, ok := writer.(*maskingWriter); ok {
		masked.Flush()
	}
}
