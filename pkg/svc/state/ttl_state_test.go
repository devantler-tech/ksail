package state_test

import (
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveAndLoadClusterTTL(t *testing.T) {
	t.Parallel()

	clusterName := "test-ttl-" + t.Name()

	t.Cleanup(func() {
		_ = state.DeleteClusterState(clusterName)
	})

	err := state.SaveClusterTTL(clusterName, 2*time.Hour)
	require.NoError(t, err)

	loaded, err := state.LoadClusterTTL(clusterName)
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, "2h0m0s", loaded.Duration)
	assert.WithinDuration(t, time.Now().UTC().Add(2*time.Hour), loaded.ExpiresAt, 5*time.Second)
}

func TestLoadClusterTTL_NotSet(t *testing.T) {
	t.Parallel()

	clusterName := "test-ttl-missing-" + t.Name()

	t.Cleanup(func() {
		_ = state.DeleteClusterState(clusterName)
	})

	ttl, err := state.LoadClusterTTL(clusterName)
	require.ErrorIs(t, err, state.ErrTTLNotSet)
	assert.Nil(t, ttl)
}

func TestSaveClusterTTL_OverwritesExisting(t *testing.T) {
	t.Parallel()

	clusterName := "test-ttl-overwrite-" + t.Name()

	t.Cleanup(func() {
		_ = state.DeleteClusterState(clusterName)
	})

	err := state.SaveClusterTTL(clusterName, 1*time.Hour)
	require.NoError(t, err)

	err = state.SaveClusterTTL(clusterName, 3*time.Hour)
	require.NoError(t, err)

	loaded, err := state.LoadClusterTTL(clusterName)
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, "3h0m0s", loaded.Duration)
}

func TestTTLInfo_IsExpired_NotExpired(t *testing.T) {
	t.Parallel()

	ttl := &state.TTLInfo{
		ExpiresAt: time.Now().UTC().Add(1 * time.Hour),
		Duration:  "1h0m0s",
	}

	assert.False(t, ttl.IsExpired())
}

func TestTTLInfo_IsExpired_Expired(t *testing.T) {
	t.Parallel()

	ttl := &state.TTLInfo{
		ExpiresAt: time.Now().UTC().Add(-1 * time.Hour),
		Duration:  "1h0m0s",
	}

	assert.True(t, ttl.IsExpired())
}

func TestTTLInfo_Remaining_Positive(t *testing.T) {
	t.Parallel()

	ttl := &state.TTLInfo{
		ExpiresAt: time.Now().UTC().Add(30 * time.Minute),
		Duration:  "30m0s",
	}

	remaining := ttl.Remaining()

	assert.Positive(t, remaining)
	assert.LessOrEqual(t, remaining, 30*time.Minute)
}

func TestTTLInfo_Remaining_Negative(t *testing.T) {
	t.Parallel()

	ttl := &state.TTLInfo{
		ExpiresAt: time.Now().UTC().Add(-15 * time.Minute),
		Duration:  "15m0s",
	}

	remaining := ttl.Remaining()

	assert.Negative(t, remaining)
}

func TestSaveClusterTTL_NonPositiveDuration(t *testing.T) {
	t.Parallel()

	err := state.SaveClusterTTL("test-zero-ttl", 0)
	require.ErrorIs(t, err, state.ErrNonPositiveTTL)

	err = state.SaveClusterTTL("test-neg-ttl", -1*time.Hour)
	require.ErrorIs(t, err, state.ErrNonPositiveTTL)
}

func TestSaveClusterTTL_InvalidClusterName(t *testing.T) {
	t.Parallel()

	err := state.SaveClusterTTL("../invalid/name", 1*time.Hour)
	require.Error(t, err)
	require.ErrorIs(t, err, state.ErrInvalidClusterName)
}

func TestLoadClusterTTL_InvalidClusterName(t *testing.T) {
	t.Parallel()

	_, err := state.LoadClusterTTL("../invalid/name")
	require.Error(t, err)
	require.ErrorIs(t, err, state.ErrInvalidClusterName)
}
