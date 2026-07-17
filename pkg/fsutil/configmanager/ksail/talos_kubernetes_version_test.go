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
	"github.com/siderolabs/talos/pkg/machinery/config/container"
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
		"    cni: Cilium\n" +
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

	disablePatch := findTalosPatch(
		t,
		manager.DistributionConfig.Talos.Patches(),
		"disable-default-cni",
	)
	assert.Equal(
		t,
		"cluster:\n  network:\n    cni:\n      name: none\n",
		string(disablePatch.Content),
	)
}

// TestLoadConfig_TalosFallbackUsesPinnedVersionContract guards the no-scaffold
// fallback: an explicit Talos 1.14 pin must select both the multi-document base
// configuration and the matching Flannel delete patch.
//
//nolint:paralleltest // Uses t.Chdir to isolate filesystem state for config loading.
func TestLoadConfig_TalosFallbackUsesPinnedVersionContract(t *testing.T) {
	t.Chdir(t.TempDir())

	ksailConfig := "apiVersion: ksail.io/v1alpha1\n" +
		"kind: Cluster\n" +
		"spec:\n" +
		"  cluster:\n" +
		"    distribution: Talos\n" +
		"    cni: Cilium\n" +
		"    talos:\n" +
		"      version: v1.14.0-alpha.2\n"
	require.NoError(t, os.WriteFile("ksail.yaml", []byte(ksailConfig), 0o600))

	manager := configmanager.NewConfigManager(io.Discard, "")
	manager.Viper.SetConfigFile("ksail.yaml")

	_, err := manager.Load(configmanagerinterface.LoadOptions{})
	require.NoError(t, err)

	require.NotNil(t, manager.DistributionConfig)
	require.NotNil(t, manager.DistributionConfig.Talos)

	disablePatch := findTalosPatch(
		t,
		manager.DistributionConfig.Talos.Patches(),
		"disable-default-cni",
	)
	assert.Equal(t,
		"apiVersion: v1alpha1\nkind: KubeFlannelCNIConfig\n$patch: delete\n",
		string(disablePatch.Content),
	)

	controlPlane, ok := manager.DistributionConfig.Talos.ControlPlane().(*container.Container)
	require.True(t, ok)
	assert.True(t, hasTalosDocumentKind(controlPlane, "KubeAPIServerConfig"),
		"pinned Talos 1.14 fallback must use the multi-document version contract")
}

func findTalosPatch(
	t *testing.T,
	patches []talosconfigmanager.Patch,
	path string,
) talosconfigmanager.Patch {
	t.Helper()

	for _, patch := range patches {
		if patch.Path == path {
			return patch
		}
	}

	require.FailNow(t, "Talos patch not found", path)

	return talosconfigmanager.Patch{}
}

func hasTalosDocumentKind(config *container.Container, kind string) bool {
	for _, document := range config.Documents() {
		if document.Kind() == kind {
			return true
		}
	}

	return false
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
