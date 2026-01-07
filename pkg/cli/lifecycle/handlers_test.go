package lifecycle_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDetectDistributionFromContext_Kind tests detection of Kind distribution.
func TestDetectDistributionFromContext_Kind(t *testing.T) {
	t.Parallel()

	distribution, clusterName, err := lifecycle.DetectDistributionFromContext("kind-my-cluster")

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.DistributionKind, distribution)
	assert.Equal(t, "my-cluster", clusterName)
}

// TestDetectDistributionFromContext_K3d tests detection of K3d distribution.
func TestDetectDistributionFromContext_K3d(t *testing.T) {
	t.Parallel()

	distribution, clusterName, err := lifecycle.DetectDistributionFromContext("k3d-test-cluster")

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.DistributionK3d, distribution)
	assert.Equal(t, "test-cluster", clusterName)
}

// TestDetectDistributionFromContext_Talos tests detection of Talos distribution.
func TestDetectDistributionFromContext_Talos(t *testing.T) {
	t.Parallel()

	distribution, clusterName, err := lifecycle.DetectDistributionFromContext("admin@talos-cluster")

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.DistributionTalos, distribution)
	assert.Equal(t, "talos-cluster", clusterName)
}

// TestDetectDistributionFromContext_UnknownPattern tests handling of unknown context patterns.
func TestDetectDistributionFromContext_UnknownPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		context string
	}{
		{name: "docker-desktop", context: "docker-desktop"},
		{name: "minikube", context: "minikube"},
		{name: "empty", context: ""},
		{name: "random-context", context: "some-random-context"},
		{name: "gke-cluster", context: "gke_project_zone_cluster"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := lifecycle.DetectDistributionFromContext(testCase.context)

			require.Error(t, err)
			assert.ErrorIs(t, err, lifecycle.ErrUnknownContextPattern)
		})
	}
}

// TestDetectDistributionFromContext_EmptyClusterName tests handling of contexts with empty cluster names.
func TestDetectDistributionFromContext_EmptyClusterName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		context string
	}{
		{name: "kind_prefix_only", context: "kind-"},
		{name: "k3d_prefix_only", context: "k3d-"},
		{name: "talos_prefix_only", context: "admin@"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := lifecycle.DetectDistributionFromContext(testCase.context)

			require.Error(t, err)
			require.ErrorIs(t, err, lifecycle.ErrEmptyClusterName)
			assert.Contains(t, err.Error(), "empty cluster name")
			assert.Contains(t, err.Error(), testCase.context)
		})
	}
}

// TestDetectDistributionFromContext_AllPatterns_Snapshot uses snapshot testing
// to verify all distribution detection patterns in a comprehensive format.
func TestDetectDistributionFromContext_AllPatterns_Snapshot(t *testing.T) {
	t.Parallel()

	results := make(map[string]string)

	testCases := []string{
		"kind-local",
		"kind-production",
		"k3d-dev",
		"k3d-staging",
		"admin@talos-prod",
		"admin@homelab",
		"docker-desktop",
		"minikube",
	}

	for _, ctx := range testCases {
		dist, name, err := lifecycle.DetectDistributionFromContext(ctx)
		if err != nil {
			results[ctx] = "error: " + err.Error()
		} else {
			results[ctx] = string(dist) + ":" + name
		}
	}

	snaps.MatchSnapshot(t, results)
}

// TestExtractClusterNameFromContext_Kind tests cluster name extraction for Kind.
func TestExtractClusterNameFromContext_Kind(t *testing.T) {
	t.Parallel()

	clusterName := lifecycle.ExtractClusterNameFromContext("kind-local", v1alpha1.DistributionKind)
	assert.Equal(t, "local", clusterName)
}

// TestExtractClusterNameFromContext_K3d tests cluster name extraction for K3d.
func TestExtractClusterNameFromContext_K3d(t *testing.T) {
	t.Parallel()

	clusterName := lifecycle.ExtractClusterNameFromContext("k3d-my-app", v1alpha1.DistributionK3d)
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
			distribution: v1alpha1.DistributionK3d,
		},
		{
			name:         "k3d_context_with_kind_distribution",
			context:      "k3d-cluster",
			distribution: v1alpha1.DistributionKind,
		},
		{
			name:         "talos_context_with_kind_distribution",
			context:      "admin@cluster",
			distribution: v1alpha1.DistributionKind,
		},
		{
			name:         "random_context_with_kind_distribution",
			context:      "random-context",
			distribution: v1alpha1.DistributionKind,
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

		clusterName := lifecycle.ExtractClusterNameFromContext("", v1alpha1.DistributionKind)
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

	t.Run("ErrNoCurrentContext", func(t *testing.T) {
		t.Parallel()

		require.Error(t, lifecycle.ErrNoCurrentContext)
		assert.Contains(t, lifecycle.ErrNoCurrentContext.Error(), "no current context")
	})

	t.Run("ErrUnknownContextPattern", func(t *testing.T) {
		t.Parallel()

		require.Error(t, lifecycle.ErrUnknownContextPattern)
		assert.Contains(t, lifecycle.ErrUnknownContextPattern.Error(), "unknown distribution")
	})

	t.Run("ErrEmptyClusterName", func(t *testing.T) {
		t.Parallel()

		require.Error(t, lifecycle.ErrEmptyClusterName)
		assert.Contains(t, lifecycle.ErrEmptyClusterName.Error(), "empty cluster name")
	})

	t.Run("ErrMissingClusterProvisionerDependency", func(t *testing.T) {
		t.Parallel()

		require.Error(t, lifecycle.ErrMissingClusterProvisionerDependency)
		assert.Contains(
			t,
			lifecycle.ErrMissingClusterProvisionerDependency.Error(),
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
