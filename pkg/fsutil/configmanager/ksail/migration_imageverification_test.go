package configmanager_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	configmanagerinterface "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfigManager_LegacyTalosImageVerificationMigratesOnLoad is a cross-cutting
// integration test for plan item 5.4: a ksail.yaml using the deprecated
// spec.cluster.talos.imageVerification field is loaded through the config manager,
// which promotes the value into spec.cluster.imageVerification (now steering
// Kind/K3s too), clears the legacy field, and emits a deprecation warning. This is
// the deprecation-alias round-trip that keeps old ksail.yaml files working.
func TestConfigManager_LegacyTalosImageVerificationMigratesOnLoad(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	const yaml = `apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  cluster:
    distribution: Vanilla
    talos:
      imageVerification: Enabled
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
		t, v1alpha1.ImageVerificationEnabled, cluster.Spec.Cluster.ImageVerification,
		"legacy talos.imageVerification should be promoted to cluster.imageVerification",
	)
	assert.Empty(
		t,
		cluster.Spec.Cluster.Talos.ImageVerification, //nolint:staticcheck // deprecated alias under test
		"migration should clear the deprecated talos.imageVerification field",
	)
	assert.Contains(t, out.String(), "spec.cluster.talos.imageVerification is deprecated",
		"migration should emit a deprecation warning")
}

// TestConfigManager_NewImageVerificationLoadsOnAllDistributions verifies the new
// canonical spec.cluster.imageVerification field loads for a non-Talos distribution
// (the promotion's whole point) without any deprecation warning.
func TestConfigManager_NewImageVerificationLoadsOnAllDistributions(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	const yaml = `apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  cluster:
    distribution: K3s
    imageVerification: Enabled
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

	assert.Equal(t, v1alpha1.ImageVerificationEnabled, cluster.Spec.Cluster.ImageVerification)
	assert.NotContains(t, out.String(), "deprecated")
}
