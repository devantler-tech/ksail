package workloadwatch_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/flux"
	"github.com/devantler-tech/ksail/v7/pkg/svc/workloadwatch"
	"github.com/fsnotify/fsnotify"
)

const (
	appName  = "app"
	filePerm = 0o600
)

func TestScheduleApplyEnqueuesAfterDebounce(t *testing.T) {
	t.Parallel()

	state := &workloadwatch.DebounceState{}
	applyCh := make(chan string, 1)

	workloadwatch.ScheduleApply(state, "file.yaml", applyCh)

	select {
	case got := <-applyCh:
		if got != "file.yaml" {
			t.Fatalf("got %q, want file.yaml", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("debounce timer did not enqueue the file")
	}
}

func TestCancelPendingDebounceInvalidatesCallback(t *testing.T) {
	t.Parallel()

	state := &workloadwatch.DebounceState{}
	applyCh := make(chan string, 1)

	workloadwatch.ScheduleApply(state, "file.yaml", applyCh)
	workloadwatch.CancelPendingDebounce(state)

	select {
	case got := <-applyCh:
		t.Fatalf("expected no enqueue after cancel, got %q", got)
	case <-time.After(workloadwatch.DebounceInterval * 2):
		// Expected: cancellation bumped the generation so the callback no-ops.
	}
}

func TestEnqueueIfCurrentRespectsGeneration(t *testing.T) {
	t.Parallel()

	state := &workloadwatch.DebounceState{}
	applyCh := make(chan string, 1)

	state.Set(5, "current.yaml")

	// Stale generation: must not enqueue.
	workloadwatch.EnqueueIfCurrent(state, 4, applyCh)

	select {
	case got := <-applyCh:
		t.Fatalf("stale generation enqueued %q", got)
	default:
	}

	// Current generation: enqueues the last file.
	workloadwatch.EnqueueIfCurrent(state, 5, applyCh)

	if got := <-applyCh; got != "current.yaml" {
		t.Fatalf("got %q, want current.yaml", got)
	}
}

func TestIsRelevantEvent(t *testing.T) {
	t.Parallel()

	relevant := []fsnotify.Op{fsnotify.Write, fsnotify.Create, fsnotify.Remove, fsnotify.Rename}
	for _, op := range relevant {
		if !workloadwatch.IsRelevantEvent(fsnotify.Event{Op: op}) {
			t.Errorf("op %v should be relevant", op)
		}
	}

	if workloadwatch.IsRelevantEvent(fsnotify.Event{Op: fsnotify.Chmod}) {
		t.Error("chmod should not be relevant")
	}
}

func TestBuildAndDetectChangedFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "a.yaml")

	writeErr := os.WriteFile(file, []byte("x"), filePerm)
	if writeErr != nil {
		t.Fatal(writeErr)
	}

	snapshot := workloadwatch.BuildFileSnapshot(dir)
	if len(snapshot) != 1 {
		t.Fatalf("snapshot len = %d, want 1", len(snapshot))
	}

	if got := workloadwatch.DetectChangedFile(dir, snapshot); got != "" {
		t.Fatalf("expected no change, got %q", got)
	}

	// Modify the file with a future mod time to force a detected change.
	future := time.Now().Add(time.Hour)

	chtimesErr := os.Chtimes(file, future, future)
	if chtimesErr != nil {
		t.Fatal(chtimesErr)
	}

	if got := workloadwatch.DetectChangedFile(dir, snapshot); got != file {
		t.Fatalf("DetectChangedFile = %q, want %q", got, file)
	}
}

func TestFindKustomizationDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sub := filepath.Join(root, appName)

	mkdirErr := os.MkdirAll(sub, 0o750)
	if mkdirErr != nil {
		t.Fatal(mkdirErr)
	}

	kustomization := filepath.Join(sub, "kustomization.yaml")

	writeErr := os.WriteFile(kustomization, []byte("kind: Kustomization"), filePerm)
	if writeErr != nil {
		t.Fatal(writeErr)
	}

	hasKustomization := func(dir string) bool {
		_, err := os.Stat(filepath.Join(dir, "kustomization.yaml"))

		return err == nil
	}

	changed := filepath.Join(sub, "deployment.yaml")

	got := workloadwatch.FindKustomizationDir(changed, root, hasKustomization)
	if got != sub {
		t.Fatalf("FindKustomizationDir = %q, want %q", got, sub)
	}

	// A change with no kustomization boundary falls back to the root.
	other := t.TempDir()

	gotRoot := workloadwatch.FindKustomizationDir(
		filepath.Join(other, "x.yaml"),
		other,
		hasKustomization,
	)
	if gotRoot != other {
		t.Fatalf("FindKustomizationDir fallback = %q, want %q", gotRoot, other)
	}
}

func TestNormalizeFluxPath(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"./app":    appName,
		"app/":     appName,
		".":        "",
		"a/b/../c": "a/c",
	}

	for in, want := range cases {
		if got := workloadwatch.NormalizeFluxPath(in); got != want {
			t.Errorf("NormalizeFluxPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMatchFluxKustomizations(t *testing.T) {
	t.Parallel()

	kustomizations := []flux.KustomizationInfo{
		{Name: appName, Path: appName},
		{Name: "other", Path: "other"},
	}

	matches := workloadwatch.MatchFluxKustomizations("/root/app", "/root", kustomizations)
	if len(matches) != 1 || matches[0] != appName {
		t.Fatalf("MatchFluxKustomizations = %v, want [app]", matches)
	}
}

func TestFormatElapsed(t *testing.T) {
	t.Parallel()

	if got := workloadwatch.FormatElapsed(1500 * time.Millisecond); got != "1.5s" {
		t.Fatalf("FormatElapsed = %q, want 1.5s", got)
	}
}

// TestPollForChangesEmitsStartedMarker guards the "poll: started" readiness
// marker the workload-watch system test waits on. The decomposition once dropped
// this print, leaving the watcher functional but never signaling readiness.
func TestPollForChangesEmitsStartedMarker(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.yaml"), []byte("x: 1\n"), filePerm); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// A pre-cancelled context makes PollForChanges write the startup marker and
	// then return on the first select, without waiting for the poll interval.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var buf bytes.Buffer

	workloadwatch.PollForChanges(ctx, dir, make(chan string, 1), &buf)

	if !strings.Contains(buf.String(), "poll: started") {
		t.Fatalf("PollForChanges did not emit 'poll: started' readiness marker; got %q", buf.String())
	}
}
