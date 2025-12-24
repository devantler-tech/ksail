package talosindockerprovisioner_test

import (
	"path/filepath"
	"testing"

	talosindockerprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/talosindocker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTalosInDockerConfig_DefaultValues(t *testing.T) {
	t.Parallel()

	config := talosindockerprovisioner.NewTalosInDockerConfig()

	require.NotNil(t, config)
	assert.Equal(t, talosindockerprovisioner.DefaultClusterName, config.ClusterName)
	assert.Equal(t, talosindockerprovisioner.DefaultPatchesDir, config.PatchesDir)
	assert.Equal(t, talosindockerprovisioner.DefaultTalosImage, config.TalosImage)
	assert.Equal(t, talosindockerprovisioner.DefaultControlPlaneNodes, config.ControlPlaneNodes)
	assert.Equal(t, talosindockerprovisioner.DefaultWorkerNodes, config.WorkerNodes)
	assert.Equal(t, talosindockerprovisioner.DefaultNetworkCIDR, config.NetworkCIDR)
}

func TestTalosInDockerConfig_WithClusterName(t *testing.T) {
	t.Parallel()

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithClusterName("my-cluster")

	assert.Equal(t, "my-cluster", config.ClusterName)
}

func TestTalosInDockerConfig_WithClusterName_EmptyIgnored(t *testing.T) {
	t.Parallel()

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithClusterName("")

	assert.Equal(t, talosindockerprovisioner.DefaultClusterName, config.ClusterName)
}

func TestTalosInDockerConfig_WithPatchesDir(t *testing.T) {
	t.Parallel()

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithPatchesDir("custom-patches")

	assert.Equal(t, "custom-patches", config.PatchesDir)
	assert.Equal(t, filepath.Join("custom-patches", "cluster"), config.ClusterPatchesDir)
	assert.Equal(
		t,
		filepath.Join("custom-patches", "control-planes"),
		config.ControlPlanePatchesDir,
	)
	assert.Equal(t, filepath.Join("custom-patches", "workers"), config.WorkerPatchesDir)
}

func TestTalosInDockerConfig_WithPatchesDir_EmptyIgnored(t *testing.T) {
	t.Parallel()

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithPatchesDir("")

	assert.Equal(t, talosindockerprovisioner.DefaultPatchesDir, config.PatchesDir)
}

func TestTalosInDockerConfig_WithTalosImage(t *testing.T) {
	t.Parallel()

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithTalosImage("ghcr.io/siderolabs/talos:v1.9.0")

	assert.Equal(t, "ghcr.io/siderolabs/talos:v1.9.0", config.TalosImage)
}

func TestTalosInDockerConfig_WithControlPlaneNodes(t *testing.T) {
	t.Parallel()

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithControlPlaneNodes(3)

	assert.Equal(t, 3, config.ControlPlaneNodes)
}

func TestTalosInDockerConfig_WithControlPlaneNodes_ZeroIgnored(t *testing.T) {
	t.Parallel()

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithControlPlaneNodes(0)

	assert.Equal(t, talosindockerprovisioner.DefaultControlPlaneNodes, config.ControlPlaneNodes)
}

func TestTalosInDockerConfig_WithWorkerNodes(t *testing.T) {
	t.Parallel()

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithWorkerNodes(5)

	assert.Equal(t, 5, config.WorkerNodes)
}

func TestTalosInDockerConfig_WithKubeconfigPath(t *testing.T) {
	t.Parallel()

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithKubeconfigPath("/home/user/.kube/config")

	assert.Equal(t, "/home/user/.kube/config", config.KubeconfigPath)
}

func TestTalosInDockerConfig_Chaining(t *testing.T) {
	t.Parallel()

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithClusterName("test-cluster").
		WithPatchesDir("/path/to/patches").
		WithTalosImage("custom:image").
		WithControlPlaneNodes(3).
		WithWorkerNodes(2).
		WithKubeconfigPath("/tmp/kubeconfig")

	assert.Equal(t, "test-cluster", config.ClusterName)
	assert.Equal(t, "/path/to/patches", config.PatchesDir)
	assert.Equal(t, "custom:image", config.TalosImage)
	assert.Equal(t, 3, config.ControlPlaneNodes)
	assert.Equal(t, 2, config.WorkerNodes)
	assert.Equal(t, "/tmp/kubeconfig", config.KubeconfigPath)
}
