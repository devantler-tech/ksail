package talos_test

import (
	"testing"

	talosconfig "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewDefaultConfigs verifies that default Talos configs can be created.
func TestNewDefaultConfigs(t *testing.T) {
	t.Parallel()

	configs, err := talosconfig.NewDefaultConfigs()
	require.NoError(t, err)
	require.NotNil(t, configs)

	assert.Equal(t, talosconfig.DefaultClusterName, configs.GetClusterName())
	assert.NotNil(t, configs.Bundle())
	assert.NotNil(t, configs.ControlPlane())
}

// TestNewDefaultConfigsWithPatches verifies default configs with additional patches.
func TestNewDefaultConfigsWithPatches(t *testing.T) {
	t.Parallel()

	// Create an additional patch for kubelet cert rotation
	additionalPatch := talosconfig.Patch{
		Path:  "kubelet-cert-rotation",
		Scope: talosconfig.PatchScopeCluster,
		Content: []byte(`machine:
  kubelet:
    extraArgs:
      rotate-server-certificates: "true"
`),
	}

	configs, err := talosconfig.NewDefaultConfigsWithPatches([]talosconfig.Patch{additionalPatch})
	require.NoError(t, err)
	require.NotNil(t, configs)

	assert.Equal(t, talosconfig.DefaultClusterName, configs.GetClusterName())
	// The additional patch should have been applied
	assert.True(t, configs.IsKubeletCertRotationEnabled())
}

// TestNewDefaultConfigsWithPatches_Empty verifies default configs with empty patches.
func TestNewDefaultConfigsWithPatches_Empty(t *testing.T) {
	t.Parallel()

	configs, err := talosconfig.NewDefaultConfigsWithPatches(nil)
	require.NoError(t, err)
	require.NotNil(t, configs)

	assert.Equal(t, talosconfig.DefaultClusterName, configs.GetClusterName())
}

// TestConfigs_ControlPlane_NilBundle verifies nil safety of ControlPlane().
func TestConfigs_ControlPlane_NilBundle(t *testing.T) {
	t.Parallel()

	configs := &talosconfig.Configs{}
	assert.Nil(t, configs.ControlPlane())
}

// TestConfigs_Worker_NilBundle verifies nil safety of Worker().
func TestConfigs_Worker_NilBundle(t *testing.T) {
	t.Parallel()

	configs := &talosconfig.Configs{}
	assert.Nil(t, configs.Worker())
}

// TestConfigs_IsCNIDisabled_NilBundle verifies nil safety of IsCNIDisabled().
func TestConfigs_IsCNIDisabled_NilBundle(t *testing.T) {
	t.Parallel()

	configs := &talosconfig.Configs{}
	assert.False(t, configs.IsCNIDisabled())
}

// TestConfigs_IsKubeletCertRotationEnabled_NilBundle verifies nil safety.
func TestConfigs_IsKubeletCertRotationEnabled_NilBundle(t *testing.T) {
	t.Parallel()

	configs := &talosconfig.Configs{}
	assert.False(t, configs.IsKubeletCertRotationEnabled())
}

// TestConfigs_NetworkCIDR_NilBundle verifies default CIDR for nil bundle.
func TestConfigs_NetworkCIDR_NilBundle(t *testing.T) {
	t.Parallel()

	configs := &talosconfig.Configs{}
	assert.Equal(t, talosconfig.DefaultNetworkCIDR, configs.NetworkCIDR())
}

// TestConfigs_ExtractMirrorHosts_NilBundle verifies nil safety of ExtractMirrorHosts().
func TestConfigs_ExtractMirrorHosts_NilBundle(t *testing.T) {
	t.Parallel()

	configs := &talosconfig.Configs{}
	assert.Nil(t, configs.ExtractMirrorHosts())
}

// TestConfigs_KubernetesVersion_NilBundle verifies default Kubernetes version for nil bundle.
func TestConfigs_KubernetesVersion_NilBundle(t *testing.T) {
	t.Parallel()

	configs := &talosconfig.Configs{}
	assert.Equal(t, talosconfig.DefaultKubernetesVersion, configs.KubernetesVersion())
}

// TestConfigs_WithEndpoint verifies regenerating configs with a new endpoint.
func TestConfigs_WithEndpoint(t *testing.T) {
	t.Parallel()

	original, err := talosconfig.NewDefaultConfigs()
	require.NoError(t, err)

	// WithEndpoint with empty string should return the same config
	same, err := original.WithEndpoint("")
	require.NoError(t, err)
	assert.Equal(t, original.GetClusterName(), same.GetClusterName())

	// WithEndpoint with a new endpoint should regenerate
	updated, err := original.WithEndpoint("1.2.3.4")
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, original.GetClusterName(), updated.GetClusterName())
}

// TestConfigs_WithSecretsAndEndpoint_PreservesBoth verifies that calling
// WithSecrets followed by WithEndpoint produces configs that have both the
// correct secrets and the correct endpoint. This mirrors the production
// pattern in syncSecretsFromCluster / fetchClusterSecretsAndEndpoint where
// the endpoint is read from the running config (issue #4605).
func TestConfigs_WithSecretsAndEndpoint_PreservesBoth(t *testing.T) {
	t.Parallel()

	// Simulate the running cluster's config (the "source of truth")
	runningConfigs, err := talosconfig.NewDefaultConfigs()
	require.NoError(t, err)

	// Extract secrets from the running config (as fetchClusterSecretsAndEndpoint does)
	existingSecrets := runningConfigs.ExtractSecrets()
	require.NotNil(t, existingSecrets)

	// Extract endpoint from running config (as fetchClusterSecretsAndEndpoint does).
	// In production this reads runningConfig.Cluster().Endpoint().Hostname(),
	// which returns the endpoint the cluster is actually using (deterministic
	// even in HA clusters with multiple CP nodes).
	runningEndpoint := runningConfigs.ControlPlane().Cluster().Endpoint().Hostname()
	require.NotEmpty(t, runningEndpoint, "running config should have a cluster endpoint")

	// Simulate freshly loaded configs (different CA, empty endpoint)
	freshConfigs, err := talosconfig.NewDefaultConfigs()
	require.NoError(t, err)

	// Verify fresh configs have a different CA (they are independently generated)
	originalCA := freshConfigs.ControlPlane().Machine().Security().IssuingCA().Crt
	clusterCA := runningConfigs.ControlPlane().Machine().Security().IssuingCA().Crt
	assert.NotEqual(t, originalCA, clusterCA, "fresh configs should have a different CA")

	// Step 1: Sync secrets (as WithSecrets does)
	afterSecrets, err := freshConfigs.WithSecrets(existingSecrets)
	require.NoError(t, err)

	// Verify CA was synced
	syncedCA := afterSecrets.ControlPlane().Machine().Security().IssuingCA().Crt
	assert.Equal(t, clusterCA, syncedCA, "CA should match running cluster after WithSecrets")

	// Step 2: Sync endpoint extracted from running config
	afterEndpoint, err := afterSecrets.WithEndpoint(runningEndpoint)
	require.NoError(t, err)

	// Verify CA is STILL correct after WithEndpoint
	finalCA := afterEndpoint.ControlPlane().Machine().Security().IssuingCA().Crt
	assert.Equal(t, clusterCA, finalCA,
		"CA should still match running cluster after WithEndpoint")

	// Verify endpoint is in the cluster endpoint URL
	clusterEndpoint := afterEndpoint.ControlPlane().Cluster().Endpoint().String()
	assert.Contains(t, clusterEndpoint, runningEndpoint,
		"cluster endpoint should contain the synced IP")

	// Verify worker config also has the correct CA cert
	workerCA := afterEndpoint.Worker().Machine().Security().IssuingCA().Crt
	assert.Equal(t, clusterCA, workerCA,
		"worker CA cert should match running cluster")
}

// TestConfigs_ExtractMirrorHosts_WithMirrors verifies mirror host extraction from a real config.
func TestConfigs_ExtractMirrorHosts_WithMirrors(t *testing.T) {
	t.Parallel()

	// Create config with a mirror registry patch
	mirrorPatch := talosconfig.Patch{
		Path:  "mirror-registries",
		Scope: talosconfig.PatchScopeCluster,
		Content: []byte(`machine:
  registries:
    mirrors:
      docker.io:
        endpoints:
          - http://localhost:5000
`),
	}

	configs, err := talosconfig.NewDefaultConfigsWithPatches([]talosconfig.Patch{mirrorPatch})
	require.NoError(t, err)

	hosts := configs.ExtractMirrorHosts()
	assert.Contains(t, hosts, "docker.io")
}

// TestConfigs_IsCNIDisabled_WithDisabledCNI verifies CNI disabled detection.
func TestConfigs_IsCNIDisabled_WithDisabledCNI(t *testing.T) {
	t.Parallel()

	disableCNIPatch := talosconfig.Patch{
		Path:  "disable-cni",
		Scope: talosconfig.PatchScopeCluster,
		Content: []byte(`cluster:
  network:
    cni:
      name: none
`),
	}

	configs, err := talosconfig.NewDefaultConfigsWithPatches([]talosconfig.Patch{disableCNIPatch})
	require.NoError(t, err)
	assert.True(t, configs.IsCNIDisabled())
}

// TestConfigs_NetworkCIDR_WithConfig verifies network CIDR extraction from loaded config.
func TestConfigs_NetworkCIDR_WithConfig(t *testing.T) {
	t.Parallel()

	configs, err := talosconfig.NewDefaultConfigs()
	require.NoError(t, err)

	cidr := configs.NetworkCIDR()
	assert.NotEmpty(t, cidr)
}

// TestConfigs_WithName_Regeneration verifies regenerating configs with a new cluster name.
func TestConfigs_WithName_Regeneration(t *testing.T) {
	t.Parallel()

	original, err := talosconfig.NewDefaultConfigs()
	require.NoError(t, err)

	// WithName with empty should return same instance
	same, err := original.WithName("")
	require.NoError(t, err)
	assert.Equal(t, original, same)

	// WithName with same name should return same instance
	same, err = original.WithName(talosconfig.DefaultClusterName)
	require.NoError(t, err)
	assert.Equal(t, original, same)

	// WithName with different name should regenerate
	updated, err := original.WithName("new-cluster")
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, "new-cluster", updated.GetClusterName())
	// Original should be unchanged
	assert.Equal(t, talosconfig.DefaultClusterName, original.GetClusterName())
}

// TestConfigs_Worker_WithBundle verifies Worker() returns a non-nil value
// when a bundle is present.
func TestConfigs_Worker_WithBundle(t *testing.T) {
	t.Parallel()

	configs, err := talosconfig.NewDefaultConfigs()
	require.NoError(t, err)

	worker := configs.Worker()
	assert.NotNil(t, worker)
}

// TestConfigs_Bundle_WithDefault verifies Bundle() returns a non-nil value.
func TestConfigs_Bundle_WithDefault(t *testing.T) {
	t.Parallel()

	configs, err := talosconfig.NewDefaultConfigs()
	require.NoError(t, err)

	bundle := configs.Bundle()
	assert.NotNil(t, bundle)
}

// TestConfigs_KubernetesVersion_WithDefault verifies KubernetesVersion from loaded config.
func TestConfigs_KubernetesVersion_WithDefault(t *testing.T) {
	t.Parallel()

	configs, err := talosconfig.NewDefaultConfigs()
	require.NoError(t, err)

	version := configs.KubernetesVersion()
	assert.Equal(t, talosconfig.DefaultKubernetesVersion, version)
}

// TestConfigs_Patches_WithDefault verifies Patches() returns a copy.
func TestConfigs_Patches_WithDefault(t *testing.T) {
	t.Parallel()

	configs, err := talosconfig.NewDefaultConfigs()
	require.NoError(t, err)

	patches := configs.Patches()
	assert.NotNil(t, patches)
	assert.Len(t, patches, 1) // Just the allowSchedulingOnControlPlanes patch
}

// TestConfigs_Patches_Nil verifies Patches() returns nil for empty configs.
func TestConfigs_Patches_Nil(t *testing.T) {
	t.Parallel()

	configs := &talosconfig.Configs{}
	assert.Nil(t, configs.Patches())
}
