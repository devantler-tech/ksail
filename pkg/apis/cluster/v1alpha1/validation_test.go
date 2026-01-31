package v1alpha1_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
)

func TestDistributionSet_AcceptsTalos(t *testing.T) {
	t.Parallel()

	var dist v1alpha1.Distribution
	require.NoError(t, dist.Set("Talos"))
	assert.Equal(t, v1alpha1.DistributionTalos, dist)
}

func TestDistributionSet_CaseInsensitive(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		input    string
		expected v1alpha1.Distribution
	}{
		{"Talos", v1alpha1.DistributionTalos},
		{"talos", v1alpha1.DistributionTalos},
	}

	for _, testCase := range testCases {
		t.Run(testCase.input, func(t *testing.T) {
			t.Parallel()

			var dist v1alpha1.Distribution
			require.NoError(t, dist.Set(testCase.input))
			assert.Equal(t, testCase.expected, dist)
		})
	}
}

func TestDistributionSet_InvalidListsValidOptions(t *testing.T) {
	t.Parallel()

	var dist v1alpha1.Distribution

	err := dist.Set("invalid")
	require.Error(t, err)

	require.ErrorIs(t, err, v1alpha1.ErrInvalidDistribution)
	assert.Contains(t, err.Error(), "Vanilla")
	assert.Contains(t, err.Error(), "K3s")
	assert.Contains(t, err.Error(), "Talos")
}

func TestValidDistributions_IncludesTalos(t *testing.T) {
	t.Parallel()

	distributions := v1alpha1.ValidDistributions()
	assert.Contains(t, distributions, v1alpha1.DistributionTalos)
	assert.Len(t, distributions, 3) // Kind, K3s, Talos
}

func TestTalosProvidesMetricsServerByDefault_ReturnsFalse(t *testing.T) {
	t.Parallel()

	dist := v1alpha1.DistributionTalos
	assert.False(t, dist.ProvidesMetricsServerByDefault())
}

func TestTalosProvidesStorageByDefault_ReturnsFalse(t *testing.T) {
	t.Parallel()

	dist := v1alpha1.DistributionTalos
	assert.False(t, dist.ProvidesStorageByDefault())
}

func TestGitOpsEngineSet_AcceptsArgoCD(t *testing.T) {
	t.Parallel()

	var engine v1alpha1.GitOpsEngine
	require.NoError(t, engine.Set("ArgoCD"))
	assert.Equal(t, v1alpha1.GitOpsEngine("ArgoCD"), engine)
}

func TestGitOpsEngineSet_InvalidListsValidOptions(t *testing.T) {
	t.Parallel()

	var engine v1alpha1.GitOpsEngine

	err := engine.Set("invalid")
	require.Error(t, err)

	require.ErrorIs(t, err, v1alpha1.ErrInvalidGitOpsEngine)
	assert.Contains(t, err.Error(), "None")
	assert.Contains(t, err.Error(), "Flux")
	assert.Contains(t, err.Error(), "ArgoCD")
}

func TestDistribution_ContextName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		clusterName  string
		expected     string
	}{
		{
			name:         "vanilla_returns_kind_prefix",
			distribution: v1alpha1.DistributionVanilla,
			clusterName:  "my-cluster",
			expected:     "kind-my-cluster",
		},
		{
			name:         "k3s_returns_k3d_prefix",
			distribution: v1alpha1.DistributionK3s,
			clusterName:  "test-cluster",
			expected:     "k3d-test-cluster",
		},
		{
			name:         "talos_returns_admin_at_prefix",
			distribution: v1alpha1.DistributionTalos,
			clusterName:  "prod-cluster",
			expected:     "admin@prod-cluster",
		},
		{
			name:         "empty_name_returns_empty",
			distribution: v1alpha1.DistributionVanilla,
			clusterName:  "",
			expected:     "",
		},
		{
			name:         "unknown_distribution_returns_empty",
			distribution: v1alpha1.Distribution("Unknown"),
			clusterName:  "test",
			expected:     "",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := testCase.distribution.ContextName(testCase.clusterName)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

//nolint:funlen // Table-driven test with comprehensive test cases.
func TestValidateClusterName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantError bool
		errorMsg  string
	}{
		{
			name:      "valid_simple_name",
			input:     "my-cluster",
			wantError: false,
		},
		{
			name:      "valid_lowercase_letters",
			input:     "test",
			wantError: false,
		},
		{
			name:      "valid_with_numbers",
			input:     "cluster123",
			wantError: false,
		},
		{
			name:      "valid_single_letter",
			input:     "a",
			wantError: false,
		},
		{
			name:      "empty_is_valid",
			input:     "",
			wantError: false,
		},
		{
			name:      "invalid_uppercase",
			input:     "MyCluster",
			wantError: true,
			errorMsg:  "DNS-1123 compliant",
		},
		{
			name:      "invalid_starts_with_number",
			input:     "1cluster",
			wantError: true,
			errorMsg:  "must start with a letter",
		},
		{
			name:      "invalid_ends_with_hyphen",
			input:     "cluster-",
			wantError: true,
			errorMsg:  "must not end with a hyphen",
		},
		{
			name:      "invalid_special_characters",
			input:     "my_cluster",
			wantError: true,
			errorMsg:  "DNS-1123 compliant",
		},
		{
			name:      "invalid_too_long",
			input:     "this-is-a-very-long-cluster-name-that-exceeds-the-maximum-allowed-length",
			wantError: true,
			errorMsg:  "too long",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := v1alpha1.ValidateClusterName(testCase.input)
			if testCase.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.errorMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
