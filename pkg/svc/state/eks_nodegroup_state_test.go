package state_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveLoadAndDeleteEKSNodegroupState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	const clusterName = "eks-capacity-round-trip"

	want := &state.EKSNodegroupState{
		Version:     state.EKSNodegroupStateVersion,
		ClusterName: clusterName,
		Region:      "eu-north-1",
		Nodegroups: []state.EKSNodegroupCapacity{
			{Name: "workers", DesiredCapacity: 3, MinSize: 2, MaxSize: 5},
		},
	}

	require.NoError(t, state.SaveEKSNodegroupState(clusterName, "eu-north-1", want))

	got, err := state.LoadEKSNodegroupState(clusterName, "eu-north-1")
	require.NoError(t, err)
	assert.Equal(t, want, got)

	statePath := filepath.Join(
		home, ".ksail", "clusters", clusterName, "eks-nodegroups-eu-north-1.json",
	)
	info, err := os.Stat(statePath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	neighborPath := filepath.Join(filepath.Dir(statePath), "spec.json")
	require.NoError(t, os.WriteFile(neighborPath, []byte("neighbor"), 0o600))

	require.NoError(t, state.DeleteEKSNodegroupState(clusterName, "eu-north-1"))
	assert.FileExists(t, neighborPath)

	_, err = state.LoadEKSNodegroupState(clusterName, "eu-north-1")
	require.ErrorIs(t, err, state.ErrEKSNodegroupStateNotFound)
}

// TestEKSNodegroupStateIsScopedPerRegion pins the cross-region isolation the snapshot path provides:
// two same-named clusters in different regions must keep independent capacity snapshots, and
// removing one must leave the other intact.
func TestEKSNodegroupStateIsScopedPerRegion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "same-name-two-regions"

	north := &state.EKSNodegroupState{
		Version:     state.EKSNodegroupStateVersion,
		ClusterName: clusterName,
		Region:      "eu-north-1",
		Nodegroups: []state.EKSNodegroupCapacity{
			{Name: "workers", DesiredCapacity: 3, MinSize: 2, MaxSize: 5},
		},
	}
	east := &state.EKSNodegroupState{
		Version:     state.EKSNodegroupStateVersion,
		ClusterName: clusterName,
		Region:      "us-east-1",
		Nodegroups: []state.EKSNodegroupCapacity{
			{Name: "workers", DesiredCapacity: 7, MinSize: 4, MaxSize: 9},
		},
	}

	require.NoError(t, state.SaveEKSNodegroupState(clusterName, "eu-north-1", north))
	require.NoError(t, state.SaveEKSNodegroupState(clusterName, "us-east-1", east))

	// Neither region may read the other's capacities.
	gotNorth, err := state.LoadEKSNodegroupState(clusterName, "eu-north-1")
	require.NoError(t, err)
	assert.Equal(t, north, gotNorth)

	gotEast, err := state.LoadEKSNodegroupState(clusterName, "us-east-1")
	require.NoError(t, err)
	assert.Equal(t, east, gotEast)

	// Clearing one region's snapshot must not discard the other's restore data.
	require.NoError(t, state.DeleteEKSNodegroupState(clusterName, "eu-north-1"))

	_, err = state.LoadEKSNodegroupState(clusterName, "eu-north-1")
	require.ErrorIs(t, err, state.ErrEKSNodegroupStateNotFound)

	survivor, err := state.LoadEKSNodegroupState(clusterName, "us-east-1")
	require.NoError(t, err)
	assert.Equal(t, east, survivor)
}

func TestEKSNodegroupStateRejectsUnusableRegion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	for _, region := range []string{"", "  ", "../escape", "eu/north", `eu\north`} {
		_, err := state.LoadEKSNodegroupState("demo", region)
		require.ErrorIs(t, err, state.ErrInvalidRegion, "region %q must be rejected", region)
	}
}

func TestLoadEKSNodegroupStateRejectsInvalidJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	stateDir := filepath.Join(home, ".ksail", "clusters", "invalid-json")
	require.NoError(t, os.MkdirAll(stateDir, 0o700))
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(stateDir, "eks-nodegroups-eu-north-1.json"),
			[]byte("{"),
			0o600,
		),
	)

	_, err := state.LoadEKSNodegroupState("invalid-json", "eu-north-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal EKS nodegroup state")
}
