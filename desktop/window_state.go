package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// File/dir modes for the persisted window-geometry file under ~/.ksail.
const (
	windowStateDirMode  = 0o700
	windowStateFileMode = 0o600
)

// windowState is the persisted window geometry (device-independent pixels) — the window frame rect, so
// the desktop window reopens at the position and size the user last left it. It is deliberately the
// frame rect (window.Bounds() / window.SetBounds()) rather than WebviewWindowOptions: the options set
// the *content* size while every reader returns the *frame* size, so restoring through options would
// grow the window by the title-bar height each launch, and options default to re-centering (ignoring
// X/Y). Bounds()/SetBounds() are symmetric on the frame, so this round-trips exactly.
type windowState struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// valid reports whether the state has a usable size. A zero/negative size means there is no saved
// geometry (or a bounds read taken while the window was being torn down), so callers fall back to the
// built-in defaults rather than restore a zero-size or off-screen window.
func (s windowState) valid() bool {
	return s.Width > 0 && s.Height > 0
}

// windowStatePath returns ~/.ksail/desktop-window.json, the geometry persistence file.
func windowStatePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, ".ksail", "desktop-window.json"), nil
}

// loadWindowState reads the saved geometry from the default path. It returns ok=false (and the caller
// uses the defaults) when the file is absent, unreadable, or malformed — a missing or garbled file must
// never block startup.
func loadWindowState() (windowState, bool) {
	path, err := windowStatePath()
	if err != nil {
		return windowState{}, false
	}

	return loadWindowStateFrom(path)
}

// loadWindowStateFrom reads and validates the geometry at an explicit path (the file-level logic, split
// out so it can be tested against a temp path without touching the real home directory).
func loadWindowStateFrom(path string) (windowState, bool) {
	//nolint:gosec // G304: the app's own fixed geometry file under the user's home dir, not user input.
	data, err := os.ReadFile(path)
	if err != nil {
		return windowState{}, false
	}

	var state windowState
	if json.Unmarshal(data, &state) != nil || !state.valid() {
		return windowState{}, false
	}

	return state, true
}

// saveWindowState persists the geometry to the default path, best-effort: any failure is ignored (it
// only means the window won't be restored next launch).
func saveWindowState(state windowState) {
	path, err := windowStatePath()
	if err != nil {
		return
	}

	saveWindowStateTo(path, state)
}

// saveWindowStateTo writes the geometry to an explicit path. Invalid (zero-size) states are skipped so
// a teardown-time read can never clobber a good saved value.
func saveWindowStateTo(path string, state windowState) {
	if !state.valid() {
		return
	}

	if os.MkdirAll(filepath.Dir(path), windowStateDirMode) != nil {
		return
	}

	data, err := json.Marshal(state)
	if err != nil {
		return
	}

	_ = os.WriteFile(path, data, windowStateFileMode)
}

// windowTracker records the window's latest geometry as the user moves/resizes it, so it can be
// persisted at shutdown. The window is destroyed before the shutdown hook runs — WebviewWindow.Bounds()
// then returns a zero Rect — so the geometry cannot be read in the shutdown hook itself; it must be
// captured live. Event callbacks may run on a different goroutine than shutdown, so access is guarded.
type windowTracker struct {
	mu      sync.Mutex
	current windowState
	hasData bool
	saveMu  sync.Mutex
}

// update records the current geometry and reports whether it changed (and is therefore worth
// persisting). Transient zero-size reads — e.g. a sample taken while the window is being torn down —
// are ignored so they cannot clobber a good value.
func (t *windowTracker) update(state windowState) bool {
	if !state.valid() {
		return false
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.hasData && t.current == state {
		return false
	}

	t.current, t.hasData = state, true

	return true
}

// persist writes the most recently recorded geometry, if any. Writes are serialized so the poll loop
// and the shutdown hook cannot interleave on the file.
func (t *windowTracker) persist() {
	t.mu.Lock()
	state, has := t.current, t.hasData
	t.mu.Unlock()

	if !has {
		return
	}

	t.saveMu.Lock()
	defer t.saveMu.Unlock()

	saveWindowState(state)
}

const (
	// windowStatePollInterval is how often the window geometry is sampled. Wails v3 alpha does not
	// reliably deliver WindowDidMove/WindowDidResize on macOS, so the geometry is polled rather than
	// event-driven; the file is rewritten only when the geometry actually changes.
	windowStatePollInterval = 2 * time.Second

	// windowRealizePollInterval / windowRealizeMaxAttempts bound the wait for the window to be realized
	// before the saved geometry is applied (~5s total). SetBounds is a no-op until app.Run() creates the
	// native window, so the restore must wait for it; the timeout stops a hung window blocking the loop.
	windowRealizePollInterval = 20 * time.Millisecond
	windowRealizeMaxAttempts  = 250
)

// trackWindowState restores the last-saved window geometry and then keeps it persisted across launches.
//
// Both the restore and the polling run on a background goroutine: the geometry APIs (SetBounds/Bounds)
// marshal onto the main thread, and calling them on the main goroutine before app.Run() starts the
// event loop would deadlock. From a separate goroutine the SetBounds call simply blocks until the loop
// is up, then applies — so the restore lands as the window first appears.
//
// Geometry is polled (not driven by WindowDidMove/Resize, which the alpha runtime does not reliably
// deliver) and persisted on change, so the saved position survives every exit path — the red button or
// ⌘Q (which on this tray app close the window but leave the process running), the tray's "Quit KSail",
// or a kill. A final flush on graceful shutdown captures any change since the last sample.
func trackWindowState(app *application.App, window *application.WebviewWindow) {
	tracker := &windowTracker{}

	go func() {
		if saved, ok := loadWindowState(); ok {
			restoreWindowGeometry(window, saved)
		}

		pollWindowGeometry(window, tracker)
	}()

	app.OnShutdown(tracker.persist)
}

// restoreWindowGeometry applies the saved frame once the native window is realized. SetBounds is a
// no-op until app.Run() creates the window impl, so it polls Bounds() (which returns a zero Rect until
// then) at a tight interval and applies the geometry as soon as the window appears, giving up after a
// short timeout so a window that never realizes can't wedge the goroutine.
func restoreWindowGeometry(window *application.WebviewWindow, saved windowState) {
	for range windowRealizeMaxAttempts {
		if window.Bounds().Width > 0 {
			window.SetBounds(application.Rect{
				X:      saved.X,
				Y:      saved.Y,
				Width:  saved.Width,
				Height: saved.Height,
			})

			return
		}

		time.Sleep(windowRealizePollInterval)
	}
}

// pollWindowGeometry samples the window's frame geometry on a ticker and persists it whenever it
// changes, until the process exits. Bounds() returns a zero Rect once the window is destroyed, which
// update ignores, so a sample taken during teardown cannot overwrite the saved value.
func pollWindowGeometry(window *application.WebviewWindow, tracker *windowTracker) {
	ticker := time.NewTicker(windowStatePollInterval)
	defer ticker.Stop()

	for range ticker.C {
		bounds := window.Bounds()
		state := windowState{X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: bounds.Height}

		if tracker.update(state) {
			tracker.persist()
		}
	}
}
