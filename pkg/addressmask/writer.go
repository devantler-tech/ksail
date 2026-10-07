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
	// an encoded blob, not an address, so only what could still begin an
	// address is held.
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

	text := w.held + string(payload)
	cut := heldTailStart(text)

	err := w.emit(text[:cut])
	if err != nil {
		return 0, err
	}

	w.held = text[cut:]
	w.scheduleFlush()

	return len(payload), nil
}

// emit masks text and passes it on. The byte written just before it, unless it
// could itself be part of an address, is put back
// in front for the masking only, so a literal is still judged by what it
// touches on its left. The caller holds the lock.
func (w *maskingWriter) emit(text string) error {
	if text == "" {
		return nil
	}

	masked := w.masker.Mask(w.before + text)[len(w.before):]
	w.before = ""

	if last := text[len(text)-1]; !isAddressByte(last) {
		w.before = text[len(text)-1:]
	}

	_, err := io.WriteString(w.writer, masked)

	return err //nolint:wrapcheck // a transparent writer returns the sink's error as is
}

// scheduleFlush arranges for held text to be passed on if no write completes
// it. The caller holds the lock.
func (w *maskingWriter) scheduleFlush() {
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}

	if w.held == "" {
		return
	}

	w.timer = time.AfterFunc(w.flushAfter, w.flush)
}

// flush passes on whatever is held. The sink's error has no caller to go to;
// the write that follows reports it again if the sink is broken.
func (w *maskingWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

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
		return len(text) - longestAddressText
	}

	// No address holds two dots in a row, so nothing up to the last such pair
	// can belong to one that is still arriving: "Waiting..." holds nothing.
	if dots := strings.LastIndex(text[start:], ".."); dots >= 0 {
		start += dots + len("..")
	}

	return start
}

func isAddressByte(char byte) bool {
	return char == '.' || char == ':' ||
		(char >= '0' && char <= '9') ||
		(char >= 'a' && char <= 'f') ||
		(char >= 'A' && char <= 'F')
}
