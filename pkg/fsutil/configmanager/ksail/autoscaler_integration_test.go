package configmanager_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	configmanagerinterface "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfigManager_AutoscalerFieldsPreservedOnLoad is a cross-cutting integration test
// that writes a ksail.yaml containing a fully-configured autoscaler section, loads it
// through the config manager pipeline (YAML read → unmarshal → defaults), and verifies
// that every autoscaler field is deserialized and preserved correctly.
func TestConfigManager_AutoscalerFieldsPreservedOnLoad(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	const yaml = `apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  cluster:
    distribution: Vanilla
    autoscaler:
      node:
        enabled: true
        maxNodesTotal: 20
        expander: LeastWaste
        scaleDownUnneededTime: 10m
        pools:
          - name: workers-fsn1
            serverType: cx23
            location: fsn1
            min: 1
            max: 5
      pod:
        horizontal: Enabled
        vertical: Disabled
`

	configPath := filepath.Join(tempDir, "ksail.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(yaml), 0o600))

	manager := configmanager.NewConfigManager(
		io.Discard, "",
		configmanager.DefaultClusterFieldSelectors()...,
	)
	manager.Viper.SetConfigFile(configPath)

	cluster, err := manager.Load(configmanagerinterface.LoadOptions{SkipValidation: true})
	require.NoError(t, err)
	require.NotNil(t, cluster)

	node := cluster.Spec.Cluster.Autoscaler.Node
	assert.True(t, node.Enabled)
	assert.Equal(t, int32(20), node.MaxNodesTotal)
	assert.Equal(
		t,
		v1alpha1.AutoscalerExpanderList{v1alpha1.AutoscalerExpanderLeastWaste},
		node.Expander,
	)
	assert.Equal(t, "10m", node.ScaleDownUnneededTime)
	require.Len(t, node.Pools, 1)
	assert.Equal(t, "workers-fsn1", node.Pools[0].Name)
	assert.Equal(t, "cx23", node.Pools[0].ServerType)
	assert.Equal(t, "fsn1", node.Pools[0].Location)
	assert.Equal(t, int32(1), node.Pools[0].Min)
	assert.Equal(t, int32(5), node.Pools[0].Max)

	pod := cluster.Spec.Cluster.Autoscaler.Pod
	assert.Equal(t, v1alpha1.PodAutoscalerHorizontalEnabled, pod.Horizontal)
	assert.Equal(t, v1alpha1.PodAutoscalerVerticalDisabled, pod.Vertical)
}

// TestConfigManager_LegacyNodeAutoscalingMigratesOnLoad is a cross-cutting integration test
// that verifies the full migration pipeline: a YAML file using the deprecated
// spec.cluster.nodeAutoscaling field is loaded through the config manager, which triggers
// migrateDeprecatedNodeAutoscaling automatically, resulting in the new
// spec.cluster.autoscaler.node.enabled field being set and the legacy field being cleared.
func TestConfigManager_LegacyNodeAutoscalingMigratesOnLoad(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	const yaml = `apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  cluster:
    distribution: Vanilla
    nodeAutoscaling: Enabled
`

	configPath := filepath.Join(tempDir, "ksail.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(yaml), 0o600))

	var out bytes.Buffer

	manager := configmanager.NewConfigManager(
		&out, "",
		configmanager.DefaultClusterFieldSelectors()...,
	)
	manager.Viper.SetConfigFile(configPath)

	cluster, err := manager.Load(configmanagerinterface.LoadOptions{SkipValidation: true})
	require.NoError(t, err)
	require.NotNil(t, cluster)

	assert.True(
		t, cluster.Spec.Cluster.Autoscaler.Node.Enabled,
		"migration should copy NodeAutoscaling=Enabled to Autoscaler.Node.Enabled",
	)
	assert.Empty(t, cluster.Spec.Cluster.NodeAutoscaling,
		"migration should clear the deprecated nodeAutoscaling field")
	assert.Contains(t, out.String(), "deprecated",
		"migration should emit a deprecation warning")
}

// TestConfigManager_NewFieldOnlyDoesNotConflictWithDeprecatedDefault is a regression test for
// https://github.com/devantler-tech/ksail/issues/4507.
//
// When a config file sets only spec.cluster.autoscaler.node.enabled (the canonical new field)
// and does NOT set spec.cluster.nodeAutoscaling, loading the config through
// NewCommandConfigManager must succeed without a "deprecated field conflicts" error.
//
// The bug: NodeAutoscalingFieldSelector previously declared DefaultValue: NodeAutoscalingDisabled.
// AddFlagsFromFields called setPflagValueDefault which eagerly wrote "Disabled" into the Config
// struct before any config file was read. When the config file then populated
// autoscaler.node.enabled=Enabled, the migration saw old="Disabled" vs new="Enabled" → conflict.
func TestConfigManager_NewFieldOnlyDoesNotConflictWithDeprecatedDefault(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	const yaml = `apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  cluster:
    distribution: Vanilla
    autoscaler:
      node:
        enabled: true
`

	configPath := filepath.Join(tempDir, "ksail.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(yaml), 0o600))

	// Use NewCommandConfigManager (via a Cobra command) so that AddFlagsFromFields runs,
	// which is what triggers the eager default injection in the buggy code path.
	cmd := &cobra.Command{Use: "test"}
	selectors := []configmanager.FieldSelector[v1alpha1.Cluster]{
		configmanager.NodeAutoscalingFieldSelector(),
		configmanager.NodeAutoscalerEnabledFieldSelector(),
	}
	mgr := configmanager.NewCommandConfigManager(cmd, selectors)
	mgr.Viper.SetConfigFile(configPath)

	cluster, err := mgr.Load(configmanagerinterface.LoadOptions{SkipValidation: true})
	require.NoError(t, err, "loading a config with only autoscaler.node.enabled must not error")
	require.NotNil(t, cluster)

	assert.True(
		t,
		cluster.Spec.Cluster.Autoscaler.Node.Enabled,
		"autoscaler.node.enabled should be Enabled as specified in the config file",
	)
	assert.Empty(t, cluster.Spec.Cluster.NodeAutoscaling,
		"deprecated nodeAutoscaling field should remain empty when not set in config")
}

// TestConfigManager_ExpanderAcceptsScalarAndList verifies that the autoscaler
// expander field accepts both the legacy scalar form (expander: LeastWaste), an
// inline comma-separated scalar (expander: "LeastNodes,LeastWaste"), and the
// YAML sequence form (expander: [LeastNodes, LeastWaste]) — all normalising into
// an ordered AutoscalerExpanderList.
func TestConfigManager_ExpanderAcceptsScalarAndList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		expander string
		want     v1alpha1.AutoscalerExpanderList
	}{
		{
			name:     "scalar",
			expander: "        expander: LeastWaste",
			want:     v1alpha1.AutoscalerExpanderList{v1alpha1.AutoscalerExpanderLeastWaste},
		},
		{
			name:     "comma_separated_scalar",
			expander: `        expander: "LeastNodes,LeastWaste"`,
			want: v1alpha1.AutoscalerExpanderList{
				v1alpha1.AutoscalerExpanderLeastNodes,
				v1alpha1.AutoscalerExpanderLeastWaste,
			},
		},
		{
			name:     "sequence",
			expander: "        expander: [LeastNodes, LeastWaste]",
			want: v1alpha1.AutoscalerExpanderList{
				v1alpha1.AutoscalerExpanderLeastNodes,
				v1alpha1.AutoscalerExpanderLeastWaste,
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assertExpanderLoadsAs(t, testCase.expander, testCase.want)
		})
	}
}

// assertExpanderLoadsAs writes a minimal ksail.yaml whose autoscaler node
// section ends with expanderLine, loads it through the config manager, and
// asserts the resulting expander list equals want.
func assertExpanderLoadsAs(
	t *testing.T,
	expanderLine string,
	want v1alpha1.AutoscalerExpanderList,
) {
	t.Helper()

	yaml := "apiVersion: ksail.io/v1alpha1\n" +
		"kind: Cluster\n" +
		"spec:\n" +
		"  cluster:\n" +
		"    distribution: Vanilla\n" +
		"    autoscaler:\n" +
		"      node:\n" +
		"        enabled: true\n" +
		expanderLine + "\n"

	configPath := filepath.Join(t.TempDir(), "ksail.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(yaml), 0o600))

	manager := configmanager.NewConfigManager(
		io.Discard, "",
		configmanager.DefaultClusterFieldSelectors()...,
	)
	manager.Viper.SetConfigFile(configPath)

	cluster, err := manager.Load(configmanagerinterface.LoadOptions{SkipValidation: true})
	require.NoError(t, err)
	require.NotNil(t, cluster)

	assert.Equal(t, want, cluster.Spec.Cluster.Autoscaler.Node.Expander)
}
