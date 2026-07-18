package state_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveLoadEKSOwnershipState(t *testing.T) {
	t.Parallel()

	want := &state.EKSOwnershipState{
		Version:     state.EKSOwnershipStateVersion,
		ClusterName: "ownership-round-trip",
		Region:      "eu-north-1",
		AccountID:   "123456789012",
		ClusterARN:  "arn:aws:eks:eu-north-1:123456789012:cluster/ownership-round-trip",
		CreatedAt:   time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC),
	}

	require.NoError(t, state.SaveEKSOwnershipState(want.ClusterName, want.Region, want))

	got, err := state.LoadEKSOwnershipState(want.ClusterName, want.Region)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestEKSOwnershipStateIsScopedPerRegion(t *testing.T) {
	t.Parallel()

	clusterName := "ownership-region-scope"
	north := &state.EKSOwnershipState{
		Version:     state.EKSOwnershipStateVersion,
		ClusterName: clusterName,
		Region:      "eu-north-1",
		AccountID:   "123456789012",
		ClusterARN:  "arn:aws:eks:eu-north-1:123456789012:cluster/" + clusterName,
		CreatedAt:   time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC),
	}
	east := &state.EKSOwnershipState{
		Version:     state.EKSOwnershipStateVersion,
		ClusterName: clusterName,
		Region:      "us-east-1",
		AccountID:   "210987654321",
		ClusterARN:  "arn:aws:eks:us-east-1:210987654321:cluster/" + clusterName,
		CreatedAt:   time.Date(2026, 7, 18, 12, 1, 0, 0, time.UTC),
	}

	require.NoError(t, state.SaveEKSOwnershipState(clusterName, north.Region, north))
	require.NoError(t, state.SaveEKSOwnershipState(clusterName, east.Region, east))

	gotNorth, err := state.LoadEKSOwnershipState(clusterName, north.Region)
	require.NoError(t, err)
	assert.Equal(t, north, gotNorth)

	gotEast, err := state.LoadEKSOwnershipState(clusterName, east.Region)
	require.NoError(t, err)
	assert.Equal(t, east, gotEast)
}

func TestEKSOwnershipStateContainsNoCredentialsAndUsesPrivatePermissions(t *testing.T) {
	t.Parallel()

	const (
		clusterName = "ownership-private-state"
		region      = "eu-north-1"
	)

	snapshot := &state.EKSOwnershipState{
		Version:     state.EKSOwnershipStateVersion,
		ClusterName: clusterName,
		Region:      region,
		AccountID:   "123456789012",
		ClusterARN:  "arn:aws:eks:eu-north-1:123456789012:cluster/" + clusterName,
		CreatedAt:   time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC),
	}
	require.NoError(t, state.SaveEKSOwnershipState(clusterName, region, snapshot))

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	dir := filepath.Join(home, ".ksail", "clusters", clusterName)
	path := filepath.Join(dir, "eks-ownership-"+region+".json")

	//nolint:gosec // Every path component is fixed by this isolated test fixture.
	contents, err := os.ReadFile(path)
	require.NoError(t, err)

	for _, credentialField := range []string{
		"accessKey", "profile", "secret", "sessionToken", "token",
	} {
		assert.NotContains(t, string(contents), credentialField)
	}

	dirInfo, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())

	fileInfo, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fileInfo.Mode().Perm())
}

func TestLoadEKSOwnershipStateMissingRequiresMigration(t *testing.T) {
	t.Parallel()

	_, err := state.LoadEKSOwnershipState("legacy-without-identity", "eu-north-1")
	require.ErrorIs(t, err, state.ErrEKSOwnershipStateNotFound)
}

func TestSaveEKSOwnershipStateRejectsMalformedIdentity(t *testing.T) {
	t.Parallel()

	testCases := map[string]*state.EKSOwnershipState{
		"wrong version": {
			Version: 2, ClusterName: "demo", Region: "eu-north-1",
			AccountID:  "123456789012",
			ClusterARN: "arn:aws:eks:eu-north-1:123456789012:cluster/demo",
			CreatedAt:  time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC),
		},
		"missing account": {
			Version: state.EKSOwnershipStateVersion, ClusterName: "demo", Region: "eu-north-1",
			ClusterARN: "arn:aws:eks:eu-north-1:123456789012:cluster/demo",
			CreatedAt:  time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC),
		},
		"missing arn": {
			Version: state.EKSOwnershipStateVersion, ClusterName: "demo", Region: "eu-north-1",
			AccountID: "123456789012",
			CreatedAt: time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC),
		},
		"missing creation time": {
			Version: state.EKSOwnershipStateVersion, ClusterName: "demo", Region: "eu-north-1",
			AccountID:  "123456789012",
			ClusterARN: "arn:aws:eks:eu-north-1:123456789012:cluster/demo",
		},
	}

	for name, snapshot := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := state.SaveEKSOwnershipState("demo", "eu-north-1", snapshot)
			require.ErrorIs(t, err, state.ErrInvalidEKSOwnershipState)
		})
	}
}

func TestLoadEKSOwnershipStateRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	clusterName := "ownership-invalid-json"
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	dir := filepath.Join(home, ".ksail", "clusters", clusterName)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "eks-ownership-eu-north-1.json"),
		[]byte("{"),
		0o600,
	))

	_, err = state.LoadEKSOwnershipState(clusterName, "eu-north-1")
	require.Error(t, err)
	require.NotErrorIs(t, err, state.ErrEKSOwnershipStateNotFound)
	assert.ErrorContains(t, err, "unmarshal EKS ownership state")
}
