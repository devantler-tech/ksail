package configmanager_test

import (
	"bytes"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrateDeprecatedNodeAutoscaling_CopiesEnabledWhenNewUnset(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()

	cfg.Spec.Cluster.NodeAutoscaling = v1alpha1.NodeAutoscalingEnabled

	var out bytes.Buffer

	require.NoError(t, configmanager.MigrateDeprecatedNodeAutoscalingForTest(cfg, &out))

	assert.Equal(t, v1alpha1.NodeAutoscalerEnabledEnabled, cfg.Spec.Cluster.Autoscaler.Node.Enabled)

	assert.Empty(
		t,
		cfg.Spec.Cluster.NodeAutoscaling,
		"legacy field should be cleared after migration",
	)

	warning := out.String()
	assert.Contains(t, warning, "spec.cluster.nodeAutoscaling is deprecated")
}

func TestMigrateDeprecatedNodeAutoscaling_CopiesDisabledWhenNewUnset(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()

	cfg.Spec.Cluster.NodeAutoscaling = v1alpha1.NodeAutoscalingDisabled

	var out bytes.Buffer

	require.NoError(t, configmanager.MigrateDeprecatedNodeAutoscalingForTest(cfg, &out))

	assert.Equal(
		t,
		v1alpha1.NodeAutoscalerEnabledDisabled,
		cfg.Spec.Cluster.Autoscaler.Node.Enabled,
	)

	assert.Empty(
		t,
		cfg.Spec.Cluster.NodeAutoscaling,
		"legacy field should be cleared after migration",
	)
}

func TestMigrateDeprecatedNodeAutoscaling_NoOpWhenLegacyUnset(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.Autoscaler.Node.Enabled = v1alpha1.NodeAutoscalerEnabledEnabled

	var out bytes.Buffer

	require.NoError(t, configmanager.MigrateDeprecatedNodeAutoscalingForTest(cfg, &out))

	assert.Equal(t, v1alpha1.NodeAutoscalerEnabledEnabled, cfg.Spec.Cluster.Autoscaler.Node.Enabled)
	assert.Empty(t, out.String(), "no warning expected when legacy field unset")
}

func TestMigrateDeprecatedNodeAutoscaling_MatchingValuesAreSilent(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()

	cfg.Spec.Cluster.NodeAutoscaling = v1alpha1.NodeAutoscalingEnabled
	cfg.Spec.Cluster.Autoscaler.Node.Enabled = v1alpha1.NodeAutoscalerEnabledEnabled

	var out bytes.Buffer

	require.NoError(t, configmanager.MigrateDeprecatedNodeAutoscalingForTest(cfg, &out))

	assert.Empty(
		t,
		cfg.Spec.Cluster.NodeAutoscaling,
		"legacy field should be zeroed even when values match",
	)
	assert.Empty(t, out.String(), "no warning expected when new and legacy values are equivalent")
}

func TestMigrateDeprecatedNodeAutoscaling_ConflictReturnsError(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()

	cfg.Spec.Cluster.NodeAutoscaling = v1alpha1.NodeAutoscalingEnabled
	cfg.Spec.Cluster.Autoscaler.Node.Enabled = v1alpha1.NodeAutoscalerEnabledDisabled

	err := configmanager.MigrateDeprecatedNodeAutoscalingForTest(cfg, nil)
	require.ErrorIs(t, err, configmanager.ErrDeprecatedFieldConflict)
}

func TestMigrateDeprecatedNodeAutoscaling_NilConfigReturnsNil(t *testing.T) {
	t.Parallel()

	err := configmanager.MigrateDeprecatedNodeAutoscalingForTest(nil, nil)
	require.NoError(t, err)
}

func TestMigrateDeprecatedNodeAutoscaling_InvalidLegacyValueReturnsError(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.NodeAutoscaling = v1alpha1.NodeAutoscaling("SomethingInvalid")

	err := configmanager.MigrateDeprecatedNodeAutoscalingForTest(cfg, nil)
	require.ErrorIs(t, err, v1alpha1.ErrInvalidNodeAutoscaling)
}
