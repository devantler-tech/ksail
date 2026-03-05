package workload_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/workload"
	"github.com/fsnotify/fsnotify"
	"github.com/gkampitakis/go-snaps/snaps"
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
