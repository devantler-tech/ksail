package configmanager_test

import (
	"bytes"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrateDeprecatedNodeCounts_CopiesWhenNewUnset(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	//nolint:staticcheck // testing migration of deprecated field
	cfg.Spec.Cluster.Talos.ControlPlanes = 3
	//nolint:staticcheck // testing migration of deprecated field
	cfg.Spec.Cluster.Talos.Workers = 2

	var out bytes.Buffer

	require.NoError(t, configmanager.MigrateDeprecatedNodeCountsForTest(cfg, &out))

	assert.Equal(t, int32(3), cfg.Spec.Cluster.ControlPlanes)
	assert.Equal(t, int32(2), cfg.Spec.Cluster.Workers)
	//nolint:staticcheck // testing migration of deprecated field
	assert.Zero(t, cfg.Spec.Cluster.Talos.ControlPlanes,
		"legacy field should be zeroed after migration")
	//nolint:staticcheck // testing migration of deprecated field
	assert.Zero(t, cfg.Spec.Cluster.Talos.Workers,
		"legacy field should be zeroed after migration")

	warning := out.String()
	assert.Contains(t, warning, "spec.cluster.talos.controlPlanes is deprecated")
	assert.Contains(t, warning, "spec.cluster.talos.workers is deprecated")
}

func TestMigrateDeprecatedNodeCounts_NoOpWhenLegacyUnset(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.ControlPlanes = 5

	var out bytes.Buffer

	require.NoError(t, configmanager.MigrateDeprecatedNodeCountsForTest(cfg, &out))

	assert.Equal(t, int32(5), cfg.Spec.Cluster.ControlPlanes)
	assert.Empty(t, out.String(), "no warning expected when legacy field unset")
}

func TestMigrateDeprecatedNodeCounts_ConflictReturnsError(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.ControlPlanes = 3
	//nolint:staticcheck // testing migration of deprecated field
	cfg.Spec.Cluster.Talos.ControlPlanes = 5

	err := configmanager.MigrateDeprecatedNodeCountsForTest(cfg, nil)
	require.ErrorIs(t, err, configmanager.ErrDeprecatedFieldConflict)
}

func TestMigrateDeprecatedNodeCounts_MatchingValuesAreSilent(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.ControlPlanes = 3
	//nolint:staticcheck // testing migration of deprecated field
	cfg.Spec.Cluster.Talos.ControlPlanes = 3

	var out bytes.Buffer

	require.NoError(t, configmanager.MigrateDeprecatedNodeCountsForTest(cfg, &out))

	assert.Equal(t, int32(3), cfg.Spec.Cluster.ControlPlanes)
	//nolint:staticcheck // testing migration of deprecated field
	assert.Zero(t, cfg.Spec.Cluster.Talos.ControlPlanes,
		"legacy field should be zeroed even when values match")
	assert.Empty(t, out.String(),
		"no warning expected when new and legacy values are equal (no copy needed)")
}
