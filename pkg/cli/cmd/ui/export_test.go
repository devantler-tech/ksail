package ui

import "context"

// SetOpenBrowser overrides the browser launcher for tests and returns a function that restores the
// previous launcher. It lets command tests run without spawning a real browser.
func SetOpenBrowser(launcher func(context.Context, string) error) func() {
	previous := openBrowser
	openBrowser = launcher

	return func() { openBrowser = previous }
}
