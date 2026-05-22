package desktop

import (
	"context"
	"io"
	"os/exec"
)

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
