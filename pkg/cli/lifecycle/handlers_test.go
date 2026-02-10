package lifecycle_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExtractClusterNameFromContext_Kind tests cluster name extraction for Kind.
func TestExtractClusterNameFromContext_Kind(t *testing.T) {
	t.Parallel()

	clusterName := lifecycle.ExtractClusterNameFromContext(
		"kind-local",
		v1alpha1.DistributionVanilla,
	)
	assert.Equal(t, "local", clusterName)
}

// TestExtractClusterNameFromContext_K3d tests cluster name extraction for K3d.
func TestExtractClusterNameFromContext_K3d(t *testing.T) {
	t.Parallel()

	clusterName := lifecycle.ExtractClusterNameFromContext("k3d-my-app", v1alpha1.DistributionK3s)
	assert.Equal(t, "my-app", clusterName)
}

// TestExtractClusterNameFromContext_Talos tests cluster name extraction for Talos.
func TestExtractClusterNameFromContext_Talos(t *testing.T) {
	t.Parallel()

	clusterName := lifecycle.ExtractClusterNameFromContext(
		"admin@homelab",
		v1alpha1.DistributionTalos,
	)
	assert.Equal(t, "homelab", clusterName)
}

// TestExtractClusterNameFromContext_WrongPrefix tests behavior when context prefix doesn't match distribution.
func TestExtractClusterNameFromContext_WrongPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		context      string
		distribution v1alpha1.Distribution
	}{
		{
			name:         "kind_context_with_k3d_distribution",
			context:      "kind-cluster",
			distribution: v1alpha1.DistributionK3s,
		},
		{
			name:         "k3d_context_with_kind_distribution",
			context:      "k3d-cluster",
			distribution: v1alpha1.DistributionVanilla,
		},
		{
			name:         "talos_context_with_kind_distribution",
			context:      "admin@cluster",
			distribution: v1alpha1.DistributionVanilla,
		},
		{
			name:         "random_context_with_kind_distribution",
			context:      "random-context",
			distribution: v1alpha1.DistributionVanilla,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			clusterName := lifecycle.ExtractClusterNameFromContext(
				testCase.context,
				testCase.distribution,
			)
			assert.Empty(t, clusterName)
		})
	}
}

// TestExtractClusterNameFromContext_EmptyInputs tests handling of empty inputs.
func TestExtractClusterNameFromContext_EmptyInputs(t *testing.T) {
	t.Parallel()

	t.Run("empty_context", func(t *testing.T) {
		t.Parallel()

		clusterName := lifecycle.ExtractClusterNameFromContext("", v1alpha1.DistributionVanilla)
		assert.Empty(t, clusterName)
	})

	t.Run("unsupported_distribution", func(t *testing.T) {
		t.Parallel()

		clusterName := lifecycle.ExtractClusterNameFromContext("kind-test", "unsupported")
		assert.Empty(t, clusterName)
	})
}

// TestErrorVariables verifies that error variables are exported and properly defined.
func TestErrorVariables(t *testing.T) {
	t.Parallel()

	t.Run("ErrMissingProvisionerDependency", func(t *testing.T) {
		t.Parallel()

		require.Error(t, lifecycle.ErrMissingProvisionerDependency)
		assert.Contains(
			t,
			lifecycle.ErrMissingProvisionerDependency.Error(),
			"missing cluster provisioner",
		)
	})

	t.Run("ErrClusterConfigRequired", func(t *testing.T) {
		t.Parallel()

		require.Error(t, lifecycle.ErrClusterConfigRequired)
		assert.Contains(
			t,
			lifecycle.ErrClusterConfigRequired.Error(),
			"cluster configuration is required",
		)
	})
}
