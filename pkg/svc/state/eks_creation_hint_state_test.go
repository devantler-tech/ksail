package state_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validEKSCreationHint(clusterName, region string) *state.EKSCreationHint {
	return &state.EKSCreationHint{
		Version:     state.EKSCreationHintVersion,
		ClusterName: clusterName,
		Region:      region,
		AccountID:   "123456789012",
		CreationID:  "attempt-8324",
	}
}

func TestSaveLoadAndDeleteEKSCreationHint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	const clusterName = "eks-hint-round-trip"

	want := validEKSCreationHint(clusterName, "eu-north-1")
	require.NoError(t, state.SaveEKSCreationHint(clusterName, "eu-north-1", want))

	got, err := state.LoadEKSCreationHint(clusterName, "eu-north-1")
	require.NoError(t, err)
	assert.Equal(t, want, got)

	statePath := filepath.Join(
		home, ".ksail", "clusters", clusterName, "eks-creation-hint-eu-north-1.json",
	)
	info, err := os.Stat(statePath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	_, err = state.LoadEKSCreationHint(clusterName, "eu-west-1")
	require.ErrorIs(t, err, state.ErrEKSCreationHintNotFound, "a hint is scoped to its region")

	_, err = state.ListEKSOwnershipStates(clusterName)
	require.ErrorIs(t, err, state.ErrEKSOwnershipStateNotFound, "a hint is never ownership")

	require.NoError(t, state.DeleteEKSCreationHint(clusterName, "eu-north-1"))
	require.NoError(t, state.DeleteEKSCreationHint(clusterName, "eu-north-1"), "absence is fine")

	_, err = state.LoadEKSCreationHint(clusterName, "eu-north-1")
	require.ErrorIs(t, err, state.ErrEKSCreationHintNotFound)
}

func TestEKSCreationHintRejectsIncompleteOrMismatchedFields(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		mutate func(*state.EKSCreationHint)
	}{
		{name: "nil hint"},
		{name: "unknown version", mutate: func(h *state.EKSCreationHint) { h.Version = 2 }},
		{name: "other cluster", mutate: func(h *state.EKSCreationHint) { h.ClusterName = "other" }},
		{name: "other region", mutate: func(h *state.EKSCreationHint) { h.Region = "eu-west-1" }},
		{name: "malformed account", mutate: func(h *state.EKSCreationHint) { h.AccountID = "12" }},
		{name: "empty creation id", mutate: func(h *state.EKSCreationHint) { h.CreationID = " " }},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var hint *state.EKSCreationHint
			if testCase.mutate != nil {
				hint = validEKSCreationHint("eks-hint", "eu-north-1")
				testCase.mutate(hint)
			}

			err := state.SaveEKSCreationHint("eks-hint", "eu-north-1", hint)
			require.ErrorIs(t, err, state.ErrInvalidEKSCreationHint)
		})
	}
}

func TestLoadEKSCreationHintRejectsInvalidJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	stateDir := filepath.Join(home, ".ksail", "clusters", "invalid-hint")
	require.NoError(t, os.MkdirAll(stateDir, 0o700))
	require.NoError(t, os.WriteFile(
		filepath.Join(stateDir, "eks-creation-hint-eu-north-1.json"),
		[]byte("{"),
		0o600,
	))

	_, err := state.LoadEKSCreationHint("invalid-hint", "eu-north-1")
	require.ErrorIs(t, err, state.ErrInvalidEKSCreationHint)
}

func TestDeleteEKSRegionStateRemovesCreationHint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "eks-hint-region-delete"

	require.NoError(t, state.SaveEKSCreationHint(
		clusterName, "eu-north-1", validEKSCreationHint(clusterName, "eu-north-1"),
	))
	require.NoError(t, state.SaveEKSCreationHint(
		clusterName, "eu-west-1", validEKSCreationHint(clusterName, "eu-west-1"),
	))

	require.NoError(t, state.DeleteEKSRegionState(clusterName, "eu-north-1"))

	_, err := state.LoadEKSCreationHint(clusterName, "eu-north-1")
	require.ErrorIs(t, err, state.ErrEKSCreationHintNotFound)

	_, err = state.LoadEKSCreationHint(clusterName, "eu-west-1")
	require.NoError(t, err, "another region's hint must survive")
}
