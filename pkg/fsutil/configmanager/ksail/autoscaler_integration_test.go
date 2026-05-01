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
        enabled: Enabled
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
	assert.Equal(t, v1alpha1.NodeAutoscalerEnabledEnabled, node.Enabled)
	assert.Equal(t, int32(20), node.MaxNodesTotal)
	assert.Equal(t, v1alpha1.AutoscalerExpanderLeastWaste, node.Expander)
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

	assert.Equal(
		t, v1alpha1.NodeAutoscalerEnabledEnabled,
		cluster.Spec.Cluster.Autoscaler.Node.Enabled,
		"migration should copy NodeAutoscaling=Enabled to Autoscaler.Node.Enabled",
	)
	assert.Empty(t, cluster.Spec.Cluster.NodeAutoscaling,
		"migration should clear the deprecated nodeAutoscaling field")
	assert.Contains(t, out.String(), "deprecated",
		"migration should emit a deprecation warning")
}
