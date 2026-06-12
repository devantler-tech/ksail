package ui

import (
	"os"
	"sync"
)

// Test override state for the stdin TTY check.
var (
	//nolint:gochecknoglobals // dependency injection for tests
	ttyCheckerMu sync.RWMutex
	//nolint:gochecknoglobals // dependency injection for tests
	ttyCheckerOverride func() bool
)

// SetTTYCheckerForTests overrides the stdin TTY check for testing.
// Returns a restore function that should be called to reset the override.
func SetTTYCheckerForTests(checker func() bool) func() {
	ttyCheckerMu.Lock()

	previous := ttyCheckerOverride
	ttyCheckerOverride = checker

	ttyCheckerMu.Unlock()

	return func() {
		ttyCheckerMu.Lock()

		ttyCheckerOverride = previous

		ttyCheckerMu.Unlock()
	}
}

// StdinIsTTY returns true if stdin is connected to a terminal.
// This is used to skip interactive prompts in non-interactive environments (CI/pipelines).
func StdinIsTTY() bool {
	ttyCheckerMu.RLock()
	defer ttyCheckerMu.RUnlock()

	if ttyCheckerOverride != nil {
		return ttyCheckerOverride()
	}

	fileInfo, err := os.Stdin.Stat()
	if err != nil {
		return false
	}

	// If stdin is a character device (terminal), ModeCharDevice will be set
	return (fileInfo.Mode() & os.ModeCharDevice) != 0
}
