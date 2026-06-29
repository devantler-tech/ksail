package scaffolder_test

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil/scaffolder"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

// devcontainerPath is the relative path of the scaffolded Dev Container definition.
func devcontainerPath(dir string) string {
	return filepath.Join(
		dir,
		scaffolder.DevcontainerDir,
		scaffolder.DevcontainerConfigFile,
	)
}

func TestScaffoldGeneratesDevcontainerByDefault(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	instance := scaffolder.NewScaffolder(createKindCluster("devcontainer-default"), io.Discard, nil)

	err := instance.Scaffold(tempDir, false)
	require.NoError(t, err)

	content, err := os.ReadFile(devcontainerPath(tempDir))
	require.NoError(t, err)

	// Snapshot the exact generated devcontainer.json so its content is pinned.
	snaps.MatchSnapshot(t, string(content))
}

func TestScaffoldDevcontainerUsesClusterName(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	instance := scaffolder.NewScaffolder(createKindCluster("named"), io.Discard, nil).
		WithClusterName("my-cluster")

	err := instance.Scaffold(tempDir, false)
	require.NoError(t, err)

	content, err := os.ReadFile(devcontainerPath(tempDir))
	require.NoError(t, err)

	// The Dev Container name reflects the cluster name override.
	snaps.MatchSnapshot(t, string(content))
}

func TestScaffoldSkipsDevcontainerWhenDisabled(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	instance := scaffolder.NewScaffolder(createKindCluster("no-devcontainer"), io.Discard, nil).
		WithDevcontainer(false)

	err := instance.Scaffold(tempDir, false)
	require.NoError(t, err)

	_, statErr := os.Stat(devcontainerPath(tempDir))
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestScaffoldDevcontainerEmitsValidJSON(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	instance := scaffolder.NewScaffolder(createKindCluster("valid-json"), io.Discard, nil)

	err := instance.Scaffold(tempDir, false)
	require.NoError(t, err)

	content, err := os.ReadFile(devcontainerPath(tempDir))
	require.NoError(t, err)

	require.True(t, json.Valid(content), "generated devcontainer.json must be valid JSON")
}
