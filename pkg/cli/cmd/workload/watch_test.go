package workload_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/workload"
	"github.com/fsnotify/fsnotify"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestNewWatchCmdHasCorrectDefaults(t *testing.T) {
	t.Parallel()

	cmd := workload.NewWatchCmd()

	require.Equal(t, "watch", cmd.Use)
	require.Equal(
		t,
		"Watch for file changes and auto-apply workloads",
		cmd.Short,
	)

	pathFlag := cmd.Flags().Lookup("path")
	require.NotNil(t, pathFlag, "expected --path flag to exist")
	require.Empty(t, pathFlag.DefValue)

	initialApplyFlag := cmd.Flags().Lookup("initial-apply")
	require.NotNil(t, initialApplyFlag, "expected --initial-apply flag to exist")
	require.Equal(t, "false", initialApplyFlag.DefValue)
}

func TestWatchCmdShowsHelp(t *testing.T) {
	t.Parallel()

	cmd := workload.NewWatchCmd()

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	snaps.MatchSnapshot(t, output.String())
}

func TestWatchCmdRejectsArguments(t *testing.T) {
	t.Parallel()

	cmd := workload.NewWatchCmd()
	cmd.SetArgs([]string{"extra-arg"})

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	err := cmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown command")
}

func TestIsRelevantEvent(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		event    fsnotify.Event
		expected bool
	}{
		{
			name:     "write event is relevant",
			event:    fsnotify.Event{Name: "f.yaml", Op: fsnotify.Write},
			expected: true,
		},
		{
			name:     "create event is relevant",
			event:    fsnotify.Event{Name: "f.yaml", Op: fsnotify.Create},
			expected: true,
		},
		{
			name:     "remove event is relevant",
			event:    fsnotify.Event{Name: "f.yaml", Op: fsnotify.Remove},
			expected: true,
		},
		{
			name:     "rename event is relevant",
			event:    fsnotify.Event{Name: "f.yaml", Op: fsnotify.Rename},
			expected: true,
		},
		{
			name:     "chmod event is not relevant",
			event:    fsnotify.Event{Name: "f.yaml", Op: fsnotify.Chmod},
			expected: false,
		},
		{
			name:     "no op is not relevant",
			event:    fsnotify.Event{Name: "f.yaml", Op: 0},
			expected: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := workload.ExportIsRelevantEvent(testCase.event)
			require.Equal(t, testCase.expected, got)
		})
	}
}

func TestFormatElapsed(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{
			name:     "sub-second duration",
			duration: 300 * time.Millisecond,
			expected: "0.3s",
		},
		{
			name:     "just over one second",
			duration: 1200 * time.Millisecond,
			expected: "1.2s",
		},
		{
			name:     "whole seconds",
			duration: 5 * time.Second,
			expected: "5.0s",
		},
		{
			name:     "longer apply",
			duration: 45500 * time.Millisecond,
			expected: "45.5s",
		},
		{
			name:     "zero duration",
			duration: 0,
			expected: "0.0s",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := workload.ExportFormatElapsed(testCase.duration)
			require.Equal(t, testCase.expected, got)
		})
	}
}


func TestResolveSourceDir(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		pathFlag string
		srcDir   string
		expected string
	}{
		{
			name:     "flag takes precedence",
			pathFlag: "./custom",
			srcDir:   "configured",
			expected: "./custom",
		},
		{
			name:     "config fallback",
			pathFlag: "",
			srcDir:   "from-config",
			expected: "from-config",
		},
		{
			name:     "default when both empty",
			pathFlag: "",
			srcDir:   "",
			expected: v1alpha1.DefaultSourceDirectory,
		},
		{
			name:     "whitespace-only flag uses config",
			pathFlag: "   ",
			srcDir:   "from-config",
			expected: "from-config",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cfg := &v1alpha1.Cluster{}
			cfg.Spec.Workload.SourceDirectory = testCase.srcDir

			got := workload.ExportResolveSourceDir(cfg, testCase.pathFlag)
			require.Equal(t, testCase.expected, got)
		})
	}
}

func TestAddRecursiveWatchesSubdirectories(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	subDir := filepath.Join(tmpDir, "sub")
	require.NoError(t, os.MkdirAll(subDir, 0o750))

	nestedDir := filepath.Join(subDir, "nested")
	require.NoError(t, os.MkdirAll(nestedDir, 0o750))

	// Create a file to ensure files are skipped (only dirs watched).
	filePath := filepath.Join(tmpDir, "test.yaml")
	require.NoError(t, os.WriteFile(filePath, []byte("test"), 0o600))

	watcher, err := fsnotify.NewWatcher()
	require.NoError(t, err)

	defer func() { _ = watcher.Close() }()

	err = workload.ExportAddRecursive(watcher, tmpDir)
	require.NoError(t, err)

	// Verify the watcher has the expected directories.
	watchList := watcher.WatchList()
	require.Contains(t, watchList, tmpDir)
	require.Contains(t, watchList, subDir)
	require.Contains(t, watchList, nestedDir)
}

func TestAddRecursiveFailsOnMissingDir(t *testing.T) {
	t.Parallel()

	watcher, err := fsnotify.NewWatcher()
	require.NoError(t, err)

	defer func() { _ = watcher.Close() }()

	err = workload.ExportAddRecursive(watcher, "/nonexistent/path")
	require.Error(t, err)
}

func TestCancelPendingDebounce(t *testing.T) {
	t.Parallel()

	t.Run("increments_generation", func(t *testing.T) {
		t.Parallel()

		state := workload.ExportNewDebounceState()
		workload.ExportCancelPendingDebounce(state)

		require.Equal(t, uint64(1), workload.ExportGetGeneration(state))
	})

	t.Run("each_call_increments_generation", func(t *testing.T) {
		t.Parallel()

		state := workload.ExportNewDebounceState()
		workload.ExportCancelPendingDebounce(state)
		workload.ExportCancelPendingDebounce(state)
		workload.ExportCancelPendingDebounce(state)

		require.Equal(t, uint64(3), workload.ExportGetGeneration(state))
	})

	t.Run("nil_timer_does_not_panic", func(t *testing.T) {
		t.Parallel()

		state := workload.ExportNewDebounceState()

		require.NotPanics(t, func() {
			workload.ExportCancelPendingDebounce(state)
		})
	})
}

func TestScheduleApply(t *testing.T) {
	t.Parallel()

	t.Run("updates_last_file", func(t *testing.T) {
		t.Parallel()

		state := workload.ExportNewDebounceState()
		applyCh := make(chan string, 1)

		workload.ExportScheduleApply(state, "test.yaml", applyCh)
		workload.ExportCancelPendingDebounce(state)

		require.Equal(t, "test.yaml", workload.ExportGetLastFile(state))
	})

	t.Run("increments_generation", func(t *testing.T) {
		t.Parallel()

		state := workload.ExportNewDebounceState()
		applyCh := make(chan string, 1)

		workload.ExportScheduleApply(state, "test.yaml", applyCh)
		workload.ExportCancelPendingDebounce(state)

		// scheduleApply increments gen (→1), cancelPendingDebounce increments gen (→2).
		require.Equal(t, uint64(2), workload.ExportGetGeneration(state))
	})

	t.Run("replaces_previous_file", func(t *testing.T) {
		t.Parallel()

		state := workload.ExportNewDebounceState()
		applyCh := make(chan string, 1)

		workload.ExportScheduleApply(state, "first.yaml", applyCh)
		workload.ExportScheduleApply(state, "second.yaml", applyCh)
		workload.ExportCancelPendingDebounce(state)

		require.Equal(t, "second.yaml", workload.ExportGetLastFile(state))
	})

	t.Run("enqueues_file_after_debounce_interval", func(t *testing.T) {
		t.Parallel()

		state := workload.ExportNewDebounceState()
		applyCh := make(chan string, 1)

		workload.ExportScheduleApply(state, "apply.yaml", applyCh)

		select {
		case got := <-applyCh:
			require.Equal(t, "apply.yaml", got)
		case <-time.After(workload.ExportDebounceInterval + 500*time.Millisecond):
			t.Fatal("expected apply.yaml in channel within debounce interval")
		}
	})
}

func TestEnqueueIfCurrent(t *testing.T) {
	t.Parallel()

	t.Run("skips_stale_generation", func(t *testing.T) {
		t.Parallel()

		state := workload.ExportNewDebounceState()
		workload.ExportSetDebounceState(state, 5, "test.yaml")

		applyCh := make(chan string, 1)

		workload.ExportEnqueueIfCurrent(state, 4, applyCh)

		select {
		case got := <-applyCh:
			t.Fatalf("expected empty channel for stale generation, got %q", got)
		default:
			// expected: stale generation was discarded
		}
	})

	t.Run("enqueues_for_matching_generation", func(t *testing.T) {
		t.Parallel()

		state := workload.ExportNewDebounceState()
		workload.ExportSetDebounceState(state, 5, "test.yaml")

		applyCh := make(chan string, 1)

		workload.ExportEnqueueIfCurrent(state, 5, applyCh)

		select {
		case got := <-applyCh:
			require.Equal(t, "test.yaml", got)
		default:
			t.Fatal("expected test.yaml in channel for matching generation")
		}
	})

	t.Run("coalesces_stale_pending_apply", func(t *testing.T) {
		t.Parallel()

		state := workload.ExportNewDebounceState()
		workload.ExportSetDebounceState(state, 5, "latest.yaml")

		applyCh := make(chan string, 1)

		// Pre-fill channel with a stale entry.
		applyCh <- "stale.yaml"

		workload.ExportEnqueueIfCurrent(state, 5, applyCh)

		select {
		case got := <-applyCh:
			require.Equal(t, "latest.yaml", got, "stale entry should be replaced with latest file")
		default:
			t.Fatal("expected latest.yaml in channel")
		}
	})
}

func TestTryAddDirectory(t *testing.T) {
	t.Parallel()

	t.Run("skips_non_existent_path", func(t *testing.T) {
		t.Parallel()

		watcher, err := fsnotify.NewWatcher()
		require.NoError(t, err)

		defer func() { _ = watcher.Close() }()

		cmd := &cobra.Command{}

		var buf bytes.Buffer
		cmd.SetErr(&buf)

		require.NotPanics(t, func() {
			workload.ExportTryAddDirectory(watcher, "/nonexistent/path/xyz", cmd)
		})

		require.Empty(t, watcher.WatchList())
	})

	t.Run("skips_regular_file", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "test.yaml")
		require.NoError(t, os.WriteFile(filePath, []byte("content"), 0o600))

		watcher, err := fsnotify.NewWatcher()
		require.NoError(t, err)

		defer func() { _ = watcher.Close() }()

		cmd := &cobra.Command{}
		workload.ExportTryAddDirectory(watcher, filePath, cmd)

		require.Empty(t, watcher.WatchList())
	})

	t.Run("adds_directory_to_watcher", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()

		watcher, err := fsnotify.NewWatcher()
		require.NoError(t, err)

		defer func() { _ = watcher.Close() }()

		cmd := &cobra.Command{}
		workload.ExportTryAddDirectory(watcher, tmpDir, cmd)

		require.Contains(t, watcher.WatchList(), tmpDir)
	})
}

func TestFindKustomizationDirReturnsSubtree(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	subDir := filepath.Join(root, "apps", "frontend")
	require.NoError(t, os.MkdirAll(subDir, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(subDir, "kustomization.yaml"), []byte("resources: []"), 0o600,
	))

	changedFile := filepath.Join(subDir, "deployment.yaml")
	require.NoError(t, os.WriteFile(changedFile, []byte("kind: Deployment"), 0o600))

	got := workload.ExportFindKustomizationDir(changedFile, root)
	require.Equal(t, subDir, got)
}

func TestFindKustomizationDirReturnsRootWhenKustomizationAtRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "kustomization.yaml"), []byte("resources: []"), 0o600,
	))

	changedFile := filepath.Join(root, "deployment.yaml")
	require.NoError(t, os.WriteFile(changedFile, []byte("kind: Deployment"), 0o600))

	got := workload.ExportFindKustomizationDir(changedFile, root)
	require.Equal(t, root, got)
}

func TestFindKustomizationDirReturnsRootWhenNoKustomizationFound(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	subDir := filepath.Join(root, "misc")
	require.NoError(t, os.MkdirAll(subDir, 0o750))

	changedFile := filepath.Join(subDir, "notes.yaml")
	require.NoError(t, os.WriteFile(changedFile, []byte("note: true"), 0o600))

	got := workload.ExportFindKustomizationDir(changedFile, root)
	require.Equal(t, root, got)
}

func TestFindKustomizationDirWalksUpToNearest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	// Create nested structure: root/apps/kustomization.yaml and root/apps/frontend/deep/file.yaml
	appsDir := filepath.Join(root, "apps")
	require.NoError(t, os.MkdirAll(appsDir, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(appsDir, "kustomization.yaml"), []byte("resources: []"), 0o600,
	))

	deepDir := filepath.Join(appsDir, "frontend", "deep")
	require.NoError(t, os.MkdirAll(deepDir, 0o750))

	changedFile := filepath.Join(deepDir, "service.yaml")
	require.NoError(t, os.WriteFile(changedFile, []byte("kind: Service"), 0o600))

	got := workload.ExportFindKustomizationDir(changedFile, root)
	require.Equal(t, appsDir, got)
}

func TestFindKustomizationDirPrefersNearestOverParent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	// Root has kustomization.yaml
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "kustomization.yaml"), []byte("resources: []"), 0o600,
	))

	// apps/frontend also has kustomization.yaml (closer to the changed file)
	frontendDir := filepath.Join(root, "apps", "frontend")
	require.NoError(t, os.MkdirAll(frontendDir, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(frontendDir, "kustomization.yaml"), []byte("resources: []"), 0o600,
	))

	changedFile := filepath.Join(frontendDir, "deployment.yaml")
	require.NoError(t, os.WriteFile(changedFile, []byte("kind: Deployment"), 0o600))

	got := workload.ExportFindKustomizationDir(changedFile, root)
	require.Equal(t, frontendDir, got)
}

func TestFindKustomizationDirSelfEditReturnsOwnDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	subDir := filepath.Join(root, "infra")
	require.NoError(t, os.MkdirAll(subDir, 0o750))

	kustomizationFile := filepath.Join(subDir, "kustomization.yaml")
	require.NoError(t, os.WriteFile(kustomizationFile, []byte("resources: []"), 0o600))

	got := workload.ExportFindKustomizationDir(kustomizationFile, root)
	require.Equal(t, subDir, got)
}

func TestFindKustomizationDirDirectoryEventStartsAtDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	subDir := filepath.Join(root, "apps")
	require.NoError(t, os.MkdirAll(subDir, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(subDir, "kustomization.yaml"), []byte("resources: []"), 0o600,
	))

	// Pass the directory path itself (as fsnotify does for some create/rename events).
	got := workload.ExportFindKustomizationDir(subDir, root)
	require.Equal(t, subDir, got)
}

func TestFindKustomizationDirDeletedFileFallsBack(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	subDir := filepath.Join(root, "apps")
	require.NoError(t, os.MkdirAll(subDir, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(subDir, "kustomization.yaml"), []byte("resources: []"), 0o600,
	))

	// Simulate a deleted file event — the file no longer exists on disk.
	deletedFile := filepath.Join(subDir, "removed.yaml")

	got := workload.ExportFindKustomizationDir(deletedFile, root)
	require.Equal(t, subDir, got)
}
