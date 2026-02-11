package notify

import (
	"fmt"
	"io"
	"sync"
	"unicode"
	"unicode/utf8"
)

// StageSeparatingWriter wraps an io.Writer and automatically adds blank lines
// between CLI stages. It detects "title lines" (lines starting with an emoji)
// and inserts a leading newline before them when there has been previous output.
//
// This eliminates the need for manual `LeadingNewline` tracking or `firstActivityShown`
// patterns in command handlers. The writer intelligently determines when stage
// separation is needed based on the output flow.
//
// Title detection: A title line starts with an emoji (Unicode Symbol category).
// Examples: "🚀 Create cluster...", "📦 Installing components..."
//
// Usage:
//
//	writer := notify.NewStageSeparatingWriter(cmd.OutOrStdout())
//	cmd.SetOut(writer) // All cmd.Println and notify.WriteMessage calls use this
//	// Stages are now automatically separated with blank lines
type StageSeparatingWriter struct {
	underlying io.Writer
	hasWritten bool // Whether any content has been written
	mu         sync.Mutex
}

// NewStageSeparatingWriter creates a new StageSeparatingWriter wrapping the given writer.
func NewStageSeparatingWriter(underlying io.Writer) *StageSeparatingWriter {
	return &StageSeparatingWriter{
		underlying: underlying,
	}
}

// Write implements io.Writer.
// It detects title lines and automatically adds a leading newline when:
//  1. There has been previous output (hasWritten is true).
//  2. The current line starts with an emoji (title line).
func (w *StageSeparatingWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(data) == 0 {
		return 0, nil
	}

	// Check if this is a title line (starts with emoji)
	if w.hasWritten && startsWithEmoji(data) {
		// Add leading newline to separate from previous stage
		_, writeErr := w.underlying.Write([]byte{'\n'})
		if writeErr != nil {
			return 0, fmt.Errorf("failed to write stage separator: %w", writeErr)
		}
	}

	bytesWritten, err := w.underlying.Write(data)
	if bytesWritten > 0 {
		w.hasWritten = true
	}

	if err != nil {
		return bytesWritten, fmt.Errorf("failed to write data: %w", err)
	}

	return bytesWritten, nil
}

// Reset clears the hasWritten state.
// Call this to treat the next title as the first output (no leading newline).
func (w *StageSeparatingWriter) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.hasWritten = false
}

// HasWritten returns whether any content has been written.
func (w *StageSeparatingWriter) HasWritten() bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.hasWritten
}

// startsWithTitleEmoji checks if the data starts with a title emoji character.
// Title emojis are pictographic symbols (like 🚀, 📦, ⚙️) used for stage titles,
// NOT the activity symbols (►, ✔, ✗, ℹ) used for message lines.
//
// We detect title emojis by checking:
//  1. The character is in the "Other Symbol" (So) Unicode category.
//  2. The character is NOT one of our known activity symbols.
//
// This detects patterns like: "🚀 Create...", "📦 Installing...", "⚙️ Configuring...".
// But NOT: "► creating...", "✔ success", "✗ error".
func startsWithEmoji(data []byte) bool {
	if len(data) == 0 {
		return false
	}

	// Decode the first rune using standard library
	firstRune, _ := utf8.DecodeRune(data)
	if firstRune == utf8.RuneError {
		return false
	}

	// Exclude our known activity/status symbols
	// These are used for message lines, not stage titles
	switch firstRune {
	case '►', // Activity
		'✔', // Success
		'✗', // Error
		'⚠', // Warning
		'ℹ', // Info
		'✚', // Generate
		'⏲': // Timer
		return false
	}

	// Title emojis are in the "Other Symbol" category (So)
	// This includes pictographic emojis like 🚀, 📦, 🔌, 🗄️, etc.
	return unicode.Is(unicode.So, firstRune)
}
