package open

import (
	"context"
	"io"
	"os/exec"
)

// SetOpenBrowser overrides the browser launcher for tests and returns a function that restores the
// previous launcher. It lets `web` command tests run without spawning a real browser.
func SetOpenBrowser(launcher func(context.Context, string) error) func() {
	previous := openBrowser
	openBrowser = launcher

	return func() { openBrowser = previous }
}

// ErrDesktopAppNotFound is exposed for assertions in black-box tests.
var ErrDesktopAppNotFound = errDesktopAppNotFound

// LaunchForTest builds a launcher from the supplied (faked) dependencies and runs it, exposing the
// unexported resolution/launch logic to black-box tests.
func LaunchForTest(
	ctx context.Context,
	out io.Writer,
	goos string,
	executable func() (string, error),
	lookPath func(string) (string, error),
	fileExists func(string) bool,
	start func(*exec.Cmd) error,
	run func(*exec.Cmd) error,
) error {
	candidate := launcher{
		goos:       goos,
		executable: executable,
		lookPath:   lookPath,
		fileExists: fileExists,
		start:      start,
		run:        run,
	}

	return candidate.launch(ctx, out)
}
