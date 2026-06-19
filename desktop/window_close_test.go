package main

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// fakeHider records Hide() calls; it stands in for *application.WebviewWindow so the close-to-tray
// hook can be exercised without a live Wails window (which would need a GUI).
type fakeHider struct{ hideCalls int }

func (f *fakeHider) Hide() application.Window {
	f.hideCalls++

	return nil
}

// TestHideOnCloseHook_CancelsCloseAndHides verifies the WindowClosing hook cancels the close — which
// is what skips Wails' default window-destroying listener — and hides the window instead. Without the
// cancel, Wails destroys the window on close, after which the tray's Show is a no-op and Hide crashes
// the process (the two reported failures).
func TestHideOnCloseHook_CancelsCloseAndHides(t *testing.T) {
	t.Parallel()

	hider := &fakeHider{}
	event := application.NewWindowEvent()

	hideOnCloseHook(hider)(event)

	if !event.IsCancelled() {
		t.Error("close must be cancelled so Wails does not destroy the window")
	}

	if hider.hideCalls != 1 {
		t.Errorf(
			"window must be hidden exactly once (close-to-tray); got %d Hide call(s)",
			hider.hideCalls,
		)
	}
}
