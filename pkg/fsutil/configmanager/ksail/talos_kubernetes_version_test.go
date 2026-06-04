package configmanager_test

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	configmanagerinterface "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadConfig_TalosFallbackCapsKubernetesVersion guards the regression where the
// no-scaffolded-talos-dir fallback in cacheTalosConfig ignored the resolved version:
// with an older Talos version pinned and no talos/ patches dir, the default config
// must still cap the Kubernetes version to one the pinned Talos release supports.
//
//nolint:paralleltest // Uses t.Chdir to isolate filesystem state for config loading.
func TestLoadConfig_TalosFallbackCapsKubernetesVersion(t *testing.T) {
	t.Chdir(t.TempDir())

	// Intentionally do NOT create a talos/ directory, forcing the default-config fallback.
	ksailConfig := "apiVersion: ksail.io/v1alpha1\n" +
		"kind: Cluster\n" +
		"spec:\n" +
		"  cluster:\n" +
		"    distribution: Talos\n" +
		"    talos:\n" +
		"      version: v1.12.4\n"
	require.NoError(t, os.WriteFile("ksail.yaml", []byte(ksailConfig), 0o600))

	manager := configmanager.NewConfigManager(io.Discard, "")
	manager.Viper.SetConfigFile("ksail.yaml")

	_, err := manager.Load(configmanagerinterface.LoadOptions{})
	require.NoError(t, err)

	require.NotNil(t, manager.DistributionConfig)
	require.NotNil(t, manager.DistributionConfig.Talos)
	assert.Equal(t, "1.35.0", manager.DistributionConfig.Talos.KubernetesVersion(),
		"fallback default config must cap Kubernetes to the pinned Talos version's max")
}

func TestWarnKubernetesVersionCapped_WarnsWhenDefaultCapped(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionTalos
	cfg.Spec.Cluster.Talos.Version = "v1.12.4"

	var out bytes.Buffer
	// Resolved version differs from the built-in default => the default was capped.
	configmanager.WarnKubernetesVersionCappedForTest(cfg, "1.35.0", &out)

	assert.Contains(t, out.String(), "too new for the pinned Talos version v1.12.4")
	assert.Contains(t, out.String(), "1.35.0")
	assert.Contains(t, out.String(), "spec.cluster.kubernetesVersion")
}

func TestWarnKubernetesVersionCapped_SilentWhenExplicitlyPinned(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionTalos
	cfg.Spec.Cluster.Talos.Version = "v1.12.4"
	cfg.Spec.Cluster.KubernetesVersion = "1.34.0"

	var out bytes.Buffer
	configmanager.WarnKubernetesVersionCappedForTest(cfg, "1.34.0", &out)

	assert.Empty(t, out.String(), "explicit pin should suppress the capping notice")
}

func TestWarnKubernetesVersionCapped_SilentWhenNoTalosVersionPinned(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionTalos

	var out bytes.Buffer
	configmanager.WarnKubernetesVersionCappedForTest(cfg, "1.35.0", &out)

	assert.Empty(t, out.String(), "no Talos version pin means no capping decision to explain")
}

func TestWarnKubernetesVersionCapped_SilentWhenDefaultNotCapped(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionTalos
	cfg.Spec.Cluster.Talos.Version = "v1.13.2"

	var out bytes.Buffer
	// Resolved equals the default => nothing was capped, so nothing is reported.
	configmanager.WarnKubernetesVersionCappedForTest(
		cfg, talosconfigmanager.DefaultKubernetesVersion, &out,
	)

	assert.Empty(t, out.String(), "an uncapped default needs no notice")
}
