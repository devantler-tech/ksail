package talosprovisioner_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provider"
	omniprovider "github.com/devantler-tech/ksail/v5/pkg/svc/provider/omni"
	talosprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveOmniVersions_NoOpts(t *testing.T) {
	t.Parallel()

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosprovisioner.NewProvisioner(configs, nil)

	talosVersion, _ := provisioner.ResolveOmniVersionsForTest()

	// Falls back to the default Talos image tag
	assert.NotEmpty(t, talosVersion)
	assert.True(t, strings.HasPrefix(talosVersion, "v"), "expected version to start with 'v', got: %s", talosVersion)
}

func TestResolveOmniVersions_WithOmniOpts(t *testing.T) {
	t.Parallel()

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosprovisioner.NewProvisioner(configs, nil).
		WithOmniOptions(v1alpha1.OptionsOmni{
			TalosVersion:      "v1.11.2",
			KubernetesVersion: "v1.32.0",
		})

	talosVersion, kubernetesVersion := provisioner.ResolveOmniVersionsForTest()

	assert.Equal(t, "v1.11.2", talosVersion)
	assert.Equal(t, "v1.32.0", kubernetesVersion)
}

func TestResolveOmniVersions_KubernetesVersionFallsBackToConfigs(t *testing.T) {
	t.Parallel()

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosprovisioner.NewProvisioner(configs, nil).
		WithOmniOptions(v1alpha1.OptionsOmni{
			TalosVersion: "v1.11.2",
			// KubernetesVersion intentionally empty — should fall back to talosConfigs
		})

	talosVersion, kubernetesVersion := provisioner.ResolveOmniVersionsForTest()

	assert.Equal(t, "v1.11.2", talosVersion)
	// Should have resolved from talosConfigs (non-empty)
	assert.NotEmpty(t, kubernetesVersion)
}

func TestBuildOmniPatchInfos_NilConfigs(t *testing.T) {
	t.Parallel()

	// Provisioner with nil talosConfigs
	provisioner := talosprovisioner.NewProvisioner(nil, nil)

	patches := provisioner.BuildOmniPatchInfosForTest()

	assert.Nil(t, patches)
}

func TestBuildOmniPatchInfos_EmptyPatches(t *testing.T) {
	t.Parallel()

	// TalosConfigs with no patch files — patch list should be empty/nil
	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosprovisioner.NewProvisioner(configs, nil)

	patches := provisioner.BuildOmniPatchInfosForTest()

	assert.Empty(t, patches)
}

func TestBuildOmniPatchInfos_WithPatches(t *testing.T) {
	t.Parallel()

	configs := createTestTalosConfigsWithPatches(t, "test-cluster")
	provisioner := talosprovisioner.NewProvisioner(configs, nil)

	patches := provisioner.BuildOmniPatchInfosForTest()

	require.NotEmpty(t, patches)

	for _, patch := range patches {
		assert.NotEmpty(t, patch.Path)
		assert.NotNil(t, patch.Content)
		// Scope must be one of the three valid values
		isValid := patch.Scope == omniprovider.PatchScopeCluster ||
			patch.Scope == omniprovider.PatchScopeControlPlane ||
			patch.Scope == omniprovider.PatchScopeWorker
		assert.True(t, isValid, "unexpected patch scope: %v", patch.Scope)
	}
}

func TestSyncAndWaitOmniCluster_InvalidTemplate(t *testing.T) {
	t.Parallel()

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosprovisioner.NewProvisioner(configs, nil)
	nilClientProv := omniprovider.NewProvider(nil)

	// Empty TalosVersion causes BuildClusterTemplate to return ErrTalosVersionRequired
	err := provisioner.SyncAndWaitOmniClusterForTest(
		context.Background(),
		nilClientProv,
		omniprovider.TemplateParams{
			ClusterName:       "test",
			TalosVersion:      "",
			KubernetesVersion: "v1.32.0",
			ControlPlanes:     1,
		},
	)

	require.Error(t, err)
	require.ErrorIs(t, err, omniprovider.ErrTalosVersionRequired)
}

func TestSyncAndWaitOmniCluster_NilClientProviderFails(t *testing.T) {
	t.Parallel()

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosprovisioner.NewProvisioner(configs, nil)
	nilClientProv := omniprovider.NewProvider(nil)

	// Valid template but nil-client provider: CreateCluster returns ErrProviderUnavailable
	err := provisioner.SyncAndWaitOmniClusterForTest(
		context.Background(),
		nilClientProv,
		omniprovider.TemplateParams{
			ClusterName:       "test",
			TalosVersion:      "v1.11.2",
			KubernetesVersion: "v1.32.0",
			ControlPlanes:     1,
		},
	)

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
}

func TestSaveOmniKubeconfig_NilClient(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosprovisioner.NewProvisioner(configs,
		talosprovisioner.NewOptions().WithKubeconfigPath(tmpDir+"/kube.yaml"),
	)
	nilClientProv := omniprovider.NewProvider(nil)

	err := provisioner.SaveOmniKubeconfigForTest(
		context.Background(),
		nilClientProv,
		"test-cluster",
	)

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
}

func TestSaveOmniTalosconfig_NilClient(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosprovisioner.NewProvisioner(configs,
		talosprovisioner.NewOptions().WithTalosconfigPath(tmpDir+"/talos.yaml"),
	)
	nilClientProv := omniprovider.NewProvider(nil)

	err := provisioner.SaveOmniTalosconfigForTest(
		context.Background(),
		nilClientProv,
		"test-cluster",
	)

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
}

func TestGetOmniNodesByRole_NilClient(t *testing.T) {
	t.Parallel()

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosprovisioner.NewProvisioner(configs, nil).
		WithInfraProvider(omniprovider.NewProvider(nil))

	nodes, err := provisioner.GetOmniNodesByRoleForTest(context.Background(), "test-cluster")

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
	assert.Nil(t, nodes)
}

func TestSaveOmniConfig_WritesFile(t *testing.T) {
	t.Parallel()

	// Use a temp dir so tilde expansion is not needed, but verify saveOmniConfig
	// actually writes the file and handles path expansion/canonicalization.
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "subdir", "test-kube.yaml")

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosprovisioner.NewProvisioner(configs,
		talosprovisioner.NewOptions().WithKubeconfigPath(outPath),
	)

	dummyData := []byte("apiVersion: v1\nkind: Config\n")
	err := provisioner.SaveOmniConfigForTest(dummyData, outPath, "Kubeconfig")
	require.NoError(t, err)

	written, err := os.ReadFile(outPath) //nolint:gosec // test-controlled temp path
	require.NoError(t, err)
	assert.Equal(t, dummyData, written)
}
