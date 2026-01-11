package lifecycle_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateMinimalProvisioner_Kind tests creation of a minimal Kind provisioner.
func TestCreateMinimalProvisioner_Kind(t *testing.T) {
	t.Parallel()

	provisioner, err := lifecycle.CreateMinimalProvisioner(
		v1alpha1.DistributionVanilla,
		"test-cluster",
	)

	require.NoError(t, err)
	assert.NotNil(t, provisioner)
}

// TestCreateMinimalProvisioner_K3d tests creation of a minimal K3d provisioner.
func TestCreateMinimalProvisioner_K3d(t *testing.T) {
	t.Parallel()

	provisioner, err := lifecycle.CreateMinimalProvisioner(v1alpha1.DistributionK3s, "dev-cluster")

	require.NoError(t, err)
	assert.NotNil(t, provisioner)
}

// TestCreateMinimalProvisioner_Talos tests creation of a minimal Talos provisioner.
func TestCreateMinimalProvisioner_Talos(t *testing.T) {
	t.Parallel()

	provisioner, err := lifecycle.CreateMinimalProvisioner(
		v1alpha1.DistributionTalos,
		"prod-cluster",
	)

	require.NoError(t, err)
	assert.NotNil(t, provisioner)
}

// TestCreateMinimalProvisioner_UnsupportedDistribution tests handling of unsupported distributions.
func TestCreateMinimalProvisioner_UnsupportedDistribution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
	}{
		{name: "empty_distribution", distribution: ""},
		{name: "unknown_distribution", distribution: "minikube"},
		{name: "invalid_distribution", distribution: "invalid"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			provisioner, err := lifecycle.CreateMinimalProvisioner(testCase.distribution, "cluster")

			require.Error(t, err)
			assert.Nil(t, provisioner)
			assert.Contains(t, err.Error(), "unsupported distribution")
		})
	}
}

// TestCreateMinimalProvisioner_ClusterNames tests that cluster names are correctly passed through.
func TestCreateMinimalProvisioner_ClusterNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clusterName string
	}{
		{name: "simple_name", clusterName: "local"},
		{name: "hyphenated_name", clusterName: "my-production-cluster"},
		{name: "numeric_suffix", clusterName: "cluster-123"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			provisioner, err := lifecycle.CreateMinimalProvisioner(
				v1alpha1.DistributionVanilla,
				testCase.clusterName,
			)

			require.NoError(t, err)
			assert.NotNil(t, provisioner)
		})
	}
}
