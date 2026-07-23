package state_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/stretchr/testify/require"
)

// TestLoadEKSComponentStateRejectsSymlinkEscape proves a state filename cannot
// redirect the constrained read outside its per-cluster directory.
func TestLoadEKSComponentStateRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}

	const (
		clusterName = "eks-component-symlink-escape"
		region      = "eu-north-1"
	)

	home := t.TempDir()
	t.Setenv("HOME", home)

	clusterDir := filepath.Join(home, ".ksail", "clusters", clusterName)
	require.NoError(t, os.MkdirAll(clusterDir, 0o700))

	snapshot := state.EKSComponentState{
		Version:     state.EKSComponentStateVersion,
		ClusterName: clusterName,
		Region:      region,
	}
	data, err := json.Marshal(&snapshot)
	require.NoError(t, err)

	outsidePath := filepath.Join(t.TempDir(), "outside-state.json")
	require.NoError(t, os.WriteFile(outsidePath, data, 0o600))

	statePath := filepath.Join(clusterDir, "eks-components-"+region+".json")
	require.NoError(t, os.Symlink(outsidePath, statePath))

	_, err = state.LoadEKSComponentState(clusterName, region)
	require.ErrorIs(t, err, fsutil.ErrPathOutsideBase)
}

func TestDeleteEKSRegionStateRetainsOtherRegions(t *testing.T) {
	t.Parallel()

	const clusterName = "same-name-region-delete"

	for _, region := range []string{"eu-north-1", "us-east-1"} {
		snapshot := state.EKSComponentState{
			Version:     state.EKSComponentStateVersion,
			ClusterName: clusterName,
			Region:      region,
		}
		require.NoError(t, state.SaveEKSComponentState(clusterName, region, &snapshot))
	}

	require.NoError(t, state.DeleteEKSRegionState(clusterName, "eu-north-1"))

	_, err := state.LoadEKSComponentState(clusterName, "eu-north-1")
	require.ErrorIs(t, err, state.ErrEKSComponentStateNotFound)

	_, err = state.LoadEKSComponentState(clusterName, "us-east-1")
	require.NoError(t, err)
}
