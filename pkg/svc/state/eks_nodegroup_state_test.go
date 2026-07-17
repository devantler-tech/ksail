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

	require.NoError(t, state.SaveEKSNodegroupState(clusterName, want))

	got, err := state.LoadEKSNodegroupState(clusterName)
	require.NoError(t, err)
	assert.Equal(t, want, got)

	statePath := filepath.Join(home, ".ksail", "clusters", clusterName, "eks-nodegroups.json")
	info, err := os.Stat(statePath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	neighborPath := filepath.Join(filepath.Dir(statePath), "spec.json")
	require.NoError(t, os.WriteFile(neighborPath, []byte("neighbor"), 0o600))

	require.NoError(t, state.DeleteEKSNodegroupState(clusterName))
	assert.FileExists(t, neighborPath)

	_, err = state.LoadEKSNodegroupState(clusterName)
	require.ErrorIs(t, err, state.ErrEKSNodegroupStateNotFound)
}

func TestLoadEKSNodegroupStateRejectsInvalidJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	stateDir := filepath.Join(home, ".ksail", "clusters", "invalid-json")
	require.NoError(t, os.MkdirAll(stateDir, 0o700))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(stateDir, "eks-nodegroups.json"), []byte("{"), 0o600),
	)

	_, err := state.LoadEKSNodegroupState("invalid-json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal EKS nodegroup state")
}
