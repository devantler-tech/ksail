package talos_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewConfigManager(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		patchesDir        string
		clusterName       string
		kubernetesVersion string
		networkCIDR       string
		wantPatchesDir    string
		wantK8sVersion    string
		wantNetworkCIDR   string
	}{
		{
			name:              "with all parameters",
			patchesDir:        "custom-talos",
			clusterName:       "my-cluster",
			kubernetesVersion: "1.31.0",
			networkCIDR:       "10.6.0.0/24",
			wantPatchesDir:    "custom-talos",
			wantK8sVersion:    "1.31.0",
			wantNetworkCIDR:   "10.6.0.0/24",
		},
		{
			name:              "with default patches dir",
			patchesDir:        "",
			clusterName:       "test-cluster",
			kubernetesVersion: "1.30.0",
			networkCIDR:       "10.5.0.0/24",
			wantPatchesDir:    talos.DefaultPatchesDir,
			wantK8sVersion:    "1.30.0",
			wantNetworkCIDR:   "10.5.0.0/24",
		},
		{
			name:              "with default kubernetes version",
			patchesDir:        "talos",
			clusterName:       "default-k8s",
			kubernetesVersion: "",
			networkCIDR:       "10.5.0.0/24",
			wantPatchesDir:    "talos",
			wantK8sVersion:    talos.DefaultKubernetesVersion,
			wantNetworkCIDR:   "10.5.0.0/24",
		},
		{
			name:              "with default network CIDR",
			patchesDir:        "talos",
			clusterName:       "default-network",
			kubernetesVersion: "1.32.0",
			networkCIDR:       "",
			wantPatchesDir:    "talos",
			wantK8sVersion:    "1.32.0",
			wantNetworkCIDR:   talos.DefaultNetworkCIDR,
		},
		{
			name:              "with all defaults",
			patchesDir:        "",
			clusterName:       "all-defaults",
			kubernetesVersion: "",
			networkCIDR:       "",
			wantPatchesDir:    talos.DefaultPatchesDir,
			wantK8sVersion:    talos.DefaultKubernetesVersion,
			wantNetworkCIDR:   talos.DefaultNetworkCIDR,
		},
	}

	for i := range tests {
		testCase := tests[i]

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			manager := talos.NewConfigManager(
				testCase.patchesDir,
				testCase.clusterName,
				testCase.kubernetesVersion,
				testCase.networkCIDR,
			)

			require.NotNil(t, manager)
		})
	}
}

func TestConfigManager_WithAdditionalPatches(t *testing.T) {
	t.Parallel()

	manager := talos.NewConfigManager("talos", "test", "1.32.0", "10.5.0.0/24")

	patches := []talos.Patch{
		{
			Path:    "runtime-patch",
			Scope:   talos.PatchScopeCluster,
			Content: []byte("machine:\n  network:\n    hostname: test"),
		},
	}

	result := manager.WithAdditionalPatches(patches)

	assert.NotNil(t, result)
	assert.Equal(t, manager, result, "WithAdditionalPatches should return the same manager")
}

func TestConfigManager_ValidatePatchDirectory_NonExistentDirectory(t *testing.T) {
	t.Parallel()

	manager := talos.NewConfigManager("nonexistent-dir", "test", "1.32.0", "10.5.0.0/24")

	warning, err := manager.ValidatePatchDirectory()

	require.NoError(t, err)
	assert.Contains(t, warning, "Patch directory")
	assert.Contains(t, warning, "not found")
}

func TestConfigManager_ValidatePatchDirectory_EmptyDirectory(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	manager := talos.NewConfigManager(tmpDir, "test", "1.32.0", "10.5.0.0/24")

	warning, err := manager.ValidatePatchDirectory()

	require.NoError(t, err)
	assert.Empty(t, warning)
}

func TestConfigManager_ValidatePatchDirectory_ValidYAMLFiles(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	clusterDir := filepath.Join(tmpDir, "cluster")

	require.NoError(t, os.MkdirAll(clusterDir, 0o755))

	validYAML := []byte("machine:\n  network:\n    hostname: test\n")
	require.NoError(t, os.WriteFile(filepath.Join(clusterDir, "patch.yaml"), validYAML, 0o644))

	manager := talos.NewConfigManager(tmpDir, "test", "1.32.0", "10.5.0.0/24")

	warning, err := manager.ValidatePatchDirectory()

	require.NoError(t, err)
	assert.Empty(t, warning)
}

func TestConfigManager_ValidatePatchDirectory_InvalidYAMLFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	clusterDir := filepath.Join(tmpDir, "cluster")

	require.NoError(t, os.MkdirAll(clusterDir, 0o755))

	invalidYAML := []byte("invalid: yaml: content: [")
	require.NoError(t, os.WriteFile(filepath.Join(clusterDir, "bad.yaml"), invalidYAML, 0o644))

	manager := talos.NewConfigManager(tmpDir, "test", "1.32.0", "10.5.0.0/24")

	warning, err := manager.ValidatePatchDirectory()

	require.Error(t, err)
	assert.Empty(t, warning)
}

func TestConfigManager_LoadConfig_BasicLoad(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	manager := talos.NewConfigManager(tmpDir, "test-cluster", "1.32.0", "10.5.0.0/24")

	configs, err := manager.LoadConfig(nil)

	require.NoError(t, err)
	require.NotNil(t, configs)
	assert.Equal(t, "test-cluster", configs.GetClusterName())
}

func TestConfigManager_LoadConfig_Caching(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	manager := talos.NewConfigManager(tmpDir, "cached-cluster", "1.32.0", "10.5.0.0/24")

	// First load
	configs1, err1 := manager.LoadConfig(nil)
	require.NoError(t, err1)
	require.NotNil(t, configs1)

	// Second load should return cached result
	configs2, err2 := manager.LoadConfig(nil)
	require.NoError(t, err2)
	require.NotNil(t, configs2)

	// Should be the same instance
	assert.Same(t, configs1, configs2, "LoadConfig should cache results")
}

func TestConfigManager_LoadConfig_WithPatches(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	clusterDir := filepath.Join(tmpDir, "cluster")

	require.NoError(t, os.MkdirAll(clusterDir, 0o755))

	patchYAML := []byte("machine:\n  network:\n    hostname: test-node\n")
	require.NoError(t, os.WriteFile(filepath.Join(clusterDir, "hostname.yaml"), patchYAML, 0o644))

	manager := talos.NewConfigManager(tmpDir, "patched-cluster", "1.32.0", "10.5.0.0/24")

	configs, err := manager.LoadConfig(nil)

	require.NoError(t, err)
	require.NotNil(t, configs)
	assert.Equal(t, "patched-cluster", configs.GetClusterName())
}

func TestConfigManager_ValidateConfigs_ValidConfig(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	manager := talos.NewConfigManager(tmpDir, "valid-cluster", "1.32.0", "10.5.0.0/24")

	configs, err := manager.ValidateConfigs()

	require.NoError(t, err)
	require.NotNil(t, configs)
	assert.Equal(t, "valid-cluster", configs.GetClusterName())
}

func TestConfigManager_ValidateConfigs_WithInvalidYAML(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	clusterDir := filepath.Join(tmpDir, "cluster")

	require.NoError(t, os.MkdirAll(clusterDir, 0o755))

	invalidYAML := []byte("invalid: yaml: [")
	require.NoError(t, os.WriteFile(filepath.Join(clusterDir, "bad.yaml"), invalidYAML, 0o644))

	manager := talos.NewConfigManager(tmpDir, "invalid-cluster", "1.32.0", "10.5.0.0/24")

	configs, err := manager.ValidateConfigs()

	require.Error(t, err)
	assert.Nil(t, configs)
}

func TestConfigManager_ValidateConfigs_NonExistentPatchDir(t *testing.T) {
	t.Parallel()

	manager := talos.NewConfigManager("nonexistent", "test", "1.32.0", "10.5.0.0/24")

	// Should still succeed since patches are optional
	configs, err := manager.ValidateConfigs()

	require.NoError(t, err)
	require.NotNil(t, configs)
}
