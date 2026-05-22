// Package browser opens a URL in the user's default browser. It is a tiny, dependency-free
// cross-platform helper used by `ksail cluster ui` to launch the local web UI.
package browser

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
)

// Open launches the default browser pointed at url. It is best-effort and returns quickly without
// waiting for the browser to close, so callers can print the URL for manual navigation if it fails
// rather than aborting.
func Open(ctx context.Context, url string) error {
	name, args := commandFor(runtime.GOOS, url)

	//nolint:gosec // name and args derive from runtime.GOOS and an internally-built localhost URL.
	err := exec.CommandContext(ctx, name, args...).Start()
	if err != nil {
		return fmt.Errorf("open browser: %w", err)
	}

	return nil
}

// commandFor returns the platform-specific command and arguments that open url in the default
// browser. It is split from Open (which supplies runtime.GOOS) so the mapping can be unit-tested.
func commandFor(goos, url string) (string, []string) {
	switch goos {
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		return "open", []string{url}
	default:
		return "xdg-open", []string{url}
	}
}
