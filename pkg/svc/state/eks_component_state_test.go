package state_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	accountScopeClusterName = "same-name-account-scope"
	accountScopeRegion      = "eu-north-1"
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
		accountID   = "123456789012"
	)

	home := t.TempDir()
	t.Setenv("HOME", home)

	clusterDir := filepath.Join(home, ".ksail", "clusters", clusterName)
	require.NoError(t, os.MkdirAll(clusterDir, 0o700))

	snapshot := state.EKSComponentState{
		Version:     state.EKSComponentStateVersion,
		ClusterName: clusterName,
		Region:      region,
		AccountID:   accountID,
	}
	data, err := json.Marshal(&snapshot)
	require.NoError(t, err)

	outsidePath := filepath.Join(t.TempDir(), "outside-state.json")
	require.NoError(t, os.WriteFile(outsidePath, data, 0o600))

	statePath := filepath.Join(clusterDir, "eks-components-"+accountID+"-"+region+".json")
	require.NoError(t, os.Symlink(outsidePath, statePath))

	_, err = state.LoadEKSComponentState(clusterName, region, accountID)
	require.ErrorIs(t, err, fsutil.ErrPathOutsideBase)
}

func TestDeleteEKSRegionStateRetainsOtherRegions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const (
		clusterName = "same-name-region-delete"
		accountID   = "123456789012"
	)

	for _, region := range []string{"eu-north-1", "us-east-1"} {
		snapshot := state.EKSComponentState{
			Version:     state.EKSComponentStateVersion,
			ClusterName: clusterName,
			Region:      region,
			AccountID:   accountID,
		}
		require.NoError(t, state.SaveEKSComponentState(clusterName, region, &snapshot))
	}

	require.NoError(t, state.SaveClusterTTL(clusterName, time.Hour))
	require.NoError(t, state.SaveClusterSpec(clusterName, &v1alpha1.ClusterSpec{
		Distribution: v1alpha1.DistributionEKS,
		Provider:     v1alpha1.ProviderAWS,
		LocalRegistry: v1alpha1.LocalRegistry{
			Registry: "stale.example.test",
		},
	}))

	require.NoError(t, state.DeleteEKSRegionState(clusterName, "eu-north-1", accountID))

	_, err := state.LoadEKSComponentState(clusterName, "eu-north-1", accountID)
	require.ErrorIs(t, err, state.ErrEKSComponentStateNotFound)

	_, err = state.LoadEKSComponentState(clusterName, "us-east-1", accountID)
	require.NoError(t, err)

	_, err = state.LoadClusterTTL(clusterName)
	require.ErrorIs(
		t, err, state.ErrTTLNotSet,
		"deleted EKS targets must not retain a stale name-scoped TTL",
	)

	_, err = state.LoadClusterSpec(clusterName)
	require.ErrorIs(
		t, err, state.ErrStateNotFound,
		"deleted EKS targets must not retain a stale name-scoped spec baseline",
	)
}

func TestEKSComponentStateIsScopedByAccountAndRegion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const (
		clusterName = "same-name-account-scope"
		region      = "eu-north-1"
		accountA    = "123456789012"
		accountB    = "210987654321"
	)

	saveComponentOwnershipBinding(t, accountA)
	require.NoError(t, state.SaveEKSComponentState(clusterName, region, &state.EKSComponentState{
		Version:                                 state.EKSComponentStateVersion,
		ClusterName:                             clusterName,
		Region:                                  region,
		AccountID:                               accountA,
		AWSLoadBalancerControllerServiceAccount: "account-a-service-account",
	}))

	saveComponentOwnershipBinding(t, accountB)
	require.NoError(t, state.SaveEKSComponentState(clusterName, region, &state.EKSComponentState{
		Version:                                 state.EKSComponentStateVersion,
		ClusterName:                             clusterName,
		Region:                                  region,
		AccountID:                               accountB,
		AWSLoadBalancerControllerServiceAccount: "account-b-service-account",
	}))

	current, err := state.LoadEKSComponentState(clusterName, region)
	require.NoError(t, err)
	assert.Equal(t, accountB, current.AccountID)
	assert.Equal(t, "account-b-service-account", current.AWSLoadBalancerControllerServiceAccount)

	require.NoError(t, state.DeleteEKSRegionState(clusterName, region))

	saveComponentOwnershipBinding(t, accountB)

	_, err = state.LoadEKSComponentState(clusterName, region)
	require.ErrorIs(t, err, state.ErrEKSComponentStateNotFound)

	saveComponentOwnershipBinding(t, accountA)

	retained, err := state.LoadEKSComponentState(clusterName, region)
	require.NoError(t, err)
	assert.Equal(t, accountA, retained.AccountID)
	assert.Equal(t, "account-a-service-account", retained.AWSLoadBalancerControllerServiceAccount)
}

func saveComponentOwnershipBinding(t *testing.T, accountID string) {
	t.Helper()

	require.NoError(t, state.SaveEKSOwnershipState(
		accountScopeClusterName,
		accountScopeRegion,
		&state.EKSOwnershipState{
			Version:     state.EKSOwnershipStateVersion,
			ClusterName: accountScopeClusterName,
			Region:      accountScopeRegion,
			AccountID:   accountID,
			ClusterARN: "arn:aws:eks:" + accountScopeRegion + ":" + accountID +
				":cluster/" + accountScopeClusterName,
			CreatedAt:  time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC),
			AWSOptions: canonicalAWSOptions(),
		}))
}

func TestSaveEKSComponentStateRequiresIdentityForManagedRelease(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	err := state.SaveEKSComponentState(
		"managed-without-identity",
		"eu-north-1",
		&state.EKSComponentState{
			Version:                          state.EKSComponentStateVersion,
			ClusterName:                      "managed-without-identity",
			Region:                           "eu-north-1",
			AccountID:                        "123456789012",
			AWSLoadBalancerControllerManaged: true,
		},
	)

	require.ErrorIs(t, err, state.ErrInvalidEKSComponentState)
}

func TestSaveEKSComponentStateRejectsIdentityForUnmanagedRelease(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	err := state.SaveEKSComponentState(
		"unmanaged-with-identity",
		"eu-north-1",
		&state.EKSComponentState{
			Version:                                  state.EKSComponentStateVersion,
			ClusterName:                              "unmanaged-with-identity",
			Region:                                   "eu-north-1",
			AccountID:                                "123456789012",
			AWSLoadBalancerControllerReleaseIdentity: "stale-uid",
		},
	)

	require.ErrorIs(t, err, state.ErrInvalidEKSComponentState)
}
