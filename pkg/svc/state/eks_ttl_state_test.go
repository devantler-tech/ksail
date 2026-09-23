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

const (
	ttlRegionNorth = "eu-north-1"
	ttlRegionEast  = "us-east-1"
)

// TestEKSClusterTTLIsScopedByRegion pins #7039. EKS names are unique only within a region, so two
// same-named clusters in different regions each keep their own TTL.
func TestEKSClusterTTLIsScopedByRegion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "same-name-ttl"

	require.NoError(t, state.SaveEKSClusterTTL(clusterName, ttlRegionNorth, time.Hour))
	require.NoError(t, state.SaveEKSClusterTTL(clusterName, ttlRegionEast, 3*time.Hour))

	north, err := state.LoadEKSClusterTTL(clusterName, ttlRegionNorth)
	require.NoError(t, err)
	assert.Equal(t, "1h0m0s", north.Duration)

	east, err := state.LoadEKSClusterTTL(clusterName, ttlRegionEast)
	require.NoError(t, err)
	assert.Equal(t, "3h0m0s", east.Duration)

	_, err = state.LoadClusterTTL(clusterName)
	require.ErrorIs(t, err, state.ErrTTLNotSet,
		"an EKS TTL must not be written to the name-scoped file every region shares")
}

// TestDeleteEKSRegionStateKeepsOtherRegionsTTL is the acceptance test of #7039: deleting one
// region's cluster removes that region's TTL and leaves a same-named cluster's TTL in another
// region in place.
func TestDeleteEKSRegionStateKeepsOtherRegionsTTL(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "same-name-ttl-delete"

	require.NoError(t, state.SaveEKSClusterTTL(clusterName, ttlRegionNorth, time.Hour))
	require.NoError(t, state.SaveEKSClusterTTL(clusterName, ttlRegionEast, 3*time.Hour))

	require.NoError(t, state.DeleteEKSRegionState(clusterName, ttlRegionNorth))

	_, err := state.LoadEKSClusterTTL(clusterName, ttlRegionNorth)
	require.ErrorIs(t, err, state.ErrTTLNotSet, "the deleted region's TTL must be removed")

	east, err := state.LoadEKSClusterTTL(clusterName, ttlRegionEast)
	require.NoError(t, err, "another region's TTL must survive the delete")
	assert.Equal(t, "3h0m0s", east.Duration)
}

// A TTL written before TTLs were region-scoped lives in the name-scoped file. It still applies to
// a region that has no TTL of its own, so existing single-region clusters keep showing it.
func TestLoadEKSClusterTTLFallsBackToNameScopedTTL(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "legacy-ttl"

	require.NoError(t, state.SaveClusterTTL(clusterName, 2*time.Hour))

	legacy, err := state.LoadEKSClusterTTL(clusterName, ttlRegionNorth)
	require.NoError(t, err)
	assert.Equal(t, "2h0m0s", legacy.Duration)

	require.NoError(t, state.SaveEKSClusterTTL(clusterName, ttlRegionNorth, time.Hour))

	own, err := state.LoadEKSClusterTTL(clusterName, ttlRegionNorth)
	require.NoError(t, err)
	assert.Equal(t, "1h0m0s", own.Duration, "a region's own TTL wins over the name-scoped one")
}

// The name-scoped TTL cannot say which region it belongs to. A region delete removes it only when
// that region has no TTL of its own, since it may then be that region's; a region that does have
// its own TTL was created after TTLs became region-scoped, so the name-scoped TTL is another
// region's and must survive.
func TestDeleteEKSRegionStateNameScopedTTL(t *testing.T) {
	for _, testCase := range []struct {
		name           string
		deletedHasOwn  bool
		wantLegacyKept bool
	}{
		{name: "deleted_region_without_own_ttl", deletedHasOwn: false, wantLegacyKept: false},
		{name: "deleted_region_with_own_ttl", deletedHasOwn: true, wantLegacyKept: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())

			const clusterName = "legacy-ttl-delete"

			require.NoError(t, state.SaveClusterTTL(clusterName, 2*time.Hour))

			if testCase.deletedHasOwn {
				require.NoError(t, state.SaveEKSClusterTTL(clusterName, ttlRegionNorth, time.Hour))
			}

			require.NoError(t, state.DeleteEKSRegionState(clusterName, ttlRegionNorth))

			_, err := state.LoadClusterTTL(clusterName)
			if testCase.wantLegacyKept {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, state.ErrTTLNotSet)
			}
		})
	}
}

// A caller that listed an EKS cluster without knowing its region can still show its TTL when only
// one region has one, and must not guess between several.
func TestLoadEKSClusterTTLWithoutRegion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "unknown-region-ttl"

	_, err := state.LoadEKSClusterTTL(clusterName, "")
	require.ErrorIs(t, err, state.ErrTTLNotSet)

	require.NoError(t, state.SaveEKSClusterTTL(clusterName, ttlRegionNorth, time.Hour))

	only, err := state.LoadEKSClusterTTL(clusterName, "")
	require.NoError(t, err)
	assert.Equal(t, "1h0m0s", only.Duration)

	require.NoError(t, state.SaveEKSClusterTTL(clusterName, ttlRegionEast, 3*time.Hour))

	_, err = state.LoadEKSClusterTTL(clusterName, "")
	require.ErrorIs(t, err, state.ErrAmbiguousTTLRegion)
}

func TestEKSClusterTTLRejectsInvalidRegion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	for _, region := range []string{"../escape", "a/b", `a\b`, "  "} {
		require.ErrorIs(
			t, state.SaveEKSClusterTTL("region-guard", region, time.Hour), state.ErrInvalidRegion,
		)
	}

	_, err := state.LoadEKSClusterTTL("region-guard", "../escape")
	require.ErrorIs(t, err, state.ErrInvalidRegion)
}

// A file in the cluster directory that merely resembles a region-scoped TTL is not one, so it
// cannot make an unknown-region read ambiguous.
func TestLoadEKSClusterTTLWithoutRegionIgnoresUnrelatedFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	const clusterName = "unrelated-files-ttl"

	require.NoError(t, state.SaveEKSClusterTTL(clusterName, ttlRegionNorth, time.Hour))

	dir := filepath.Join(home, ".ksail", "clusters", clusterName)
	for _, name := range []string{"ttl-.json", "ttl.json.bak", "ttl-x.yaml"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o600))
	}

	only, err := state.LoadEKSClusterTTL(clusterName, "")
	require.NoError(t, err)
	assert.Equal(t, "1h0m0s", only.Duration)
}
