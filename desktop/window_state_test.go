package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadWindowStateRoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "win.json")
	want := windowState{X: 100, Y: 200, Width: 800, Height: 600}
	saveWindowStateTo(path, want)

	got, ok := loadWindowStateFrom(path)
	if !ok {
		t.Fatal("loadWindowStateFrom returned ok=false after a valid save")
	}

	if got != want {
		t.Errorf("loaded %+v, want %+v", got, want)
	}
}

func TestLoadWindowStateFromMissing(t *testing.T) {
	t.Parallel()

	if _, ok := loadWindowStateFrom(filepath.Join(t.TempDir(), "absent.json")); ok {
		t.Error("loadWindowStateFrom returned ok=true with no file present")
	}
}

func TestLoadWindowStateFromRejectsZeroSize(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "win.json")
	// saveWindowStateTo would skip this, so write it raw to prove load rejects a zero dimension too.
	writeRawWindowState(t, path, windowState{X: 10, Y: 10, Width: 0, Height: 400})

	if _, ok := loadWindowStateFrom(path); ok {
		t.Error("loadWindowStateFrom returned ok=true for a zero-width state")
	}
}

func TestLoadWindowStateFromRejectsGarbage(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "win.json")

	err := os.WriteFile(path, []byte("{not json"), windowStateFileMode)
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, ok := loadWindowStateFrom(path); ok {
		t.Error("loadWindowStateFrom returned ok=true for a malformed file")
	}
}

func TestSaveWindowStateToSkipsInvalid(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "win.json")
	saveWindowStateTo(path, windowState{Width: 0, Height: 0})

	if _, ok := loadWindowStateFrom(path); ok {
		t.Error("an invalid state was persisted; want nothing written")
	}
}

func TestWindowTrackerUpdateRecordsLatestValid(t *testing.T) {
	t.Parallel()

	tracker := &windowTracker{}
	if !tracker.update(windowState{X: 10, Y: 20, Width: 1000, Height: 700}) {
		t.Fatal("update returned false for the first valid geometry")
	}

	// A transient zero-size read must not clobber the good value, and reports no change.
	if tracker.update(windowState{X: 0, Y: 0, Width: 0, Height: 0}) {
		t.Error("update returned true for a zero-size geometry")
	}

	if !tracker.hasData {
		t.Fatal("tracker recorded no data after a valid update")
	}

	want := windowState{X: 10, Y: 20, Width: 1000, Height: 700}
	if tracker.current != want {
		t.Errorf("tracker.current = %+v, want %+v", tracker.current, want)
	}
}

func TestWindowTrackerUpdateDetectsChange(t *testing.T) {
	t.Parallel()

	tracker := &windowTracker{}
	geometry := windowState{X: 1, Y: 2, Width: 800, Height: 600}

	if !tracker.update(geometry) {
		t.Fatal("the first update should report a change")
	}

	if tracker.update(geometry) {
		t.Error("repeating identical geometry should report no change (avoids needless writes)")
	}

	if !tracker.update(windowState{X: 50, Y: 2, Width: 800, Height: 600}) {
		t.Error("a moved geometry should report a change")
	}
}

func TestWindowTrackerIgnoresZeroSizeOnly(t *testing.T) {
	t.Parallel()

	tracker := &windowTracker{}

	if tracker.update(windowState{X: 5, Y: 5, Width: 0, Height: 0}) {
		t.Error("update reported a change for a zero-size geometry")
	}

	if tracker.hasData {
		t.Error("tracker recorded a zero-size geometry; want it ignored")
	}
}

// writeRawWindowState writes a windowState directly, bypassing saveWindowStateTo's validity guard, so
// tests can stage invalid files.
func writeRawWindowState(t *testing.T, path string, state windowState) {
	t.Helper()

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	err = os.WriteFile(path, data, windowStateFileMode)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
}
