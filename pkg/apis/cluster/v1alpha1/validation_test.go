package v1alpha1_test

import (
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	assert.Contains(t, distributions, v1alpha1.DistributionVCluster)
	assert.Len(t, distributions, 4) // Vanilla, K3s, Talos, VCluster
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

func TestValidGitOpsEngines(t *testing.T) {
	t.Parallel()

	engines := v1alpha1.ValidGitOpsEngines()

	assert.Contains(t, engines, v1alpha1.GitOpsEngineNone)
	assert.Contains(t, engines, v1alpha1.GitOpsEngineFlux)
	assert.Contains(t, engines, v1alpha1.GitOpsEngineArgoCD)
	assert.Len(t, engines, 3)
}

func TestValidCNIs(t *testing.T) {
	t.Parallel()

	cnis := v1alpha1.ValidCNIs()

	assert.Contains(t, cnis, v1alpha1.CNIDefault)
	assert.Contains(t, cnis, v1alpha1.CNICilium)
	assert.Contains(t, cnis, v1alpha1.CNICalico)
	assert.Len(t, cnis, 3)
}

func TestValidCSIs(t *testing.T) {
	t.Parallel()

	csis := v1alpha1.ValidCSIs()

	assert.Contains(t, csis, v1alpha1.CSIDefault)
	assert.Contains(t, csis, v1alpha1.CSIEnabled)
	assert.Contains(t, csis, v1alpha1.CSIDisabled)
	assert.Len(t, csis, 3)
}

func TestValidMetricsServers(t *testing.T) {
	t.Parallel()

	servers := v1alpha1.ValidMetricsServers()

	assert.Contains(t, servers, v1alpha1.MetricsServerDefault)
	assert.Contains(t, servers, v1alpha1.MetricsServerEnabled)
	assert.Contains(t, servers, v1alpha1.MetricsServerDisabled)
	assert.Len(t, servers, 3)
}

func TestValidLoadBalancers(t *testing.T) {
	t.Parallel()

	lbs := v1alpha1.ValidLoadBalancers()

	assert.Contains(t, lbs, v1alpha1.LoadBalancerDefault)
	assert.Contains(t, lbs, v1alpha1.LoadBalancerEnabled)
	assert.Contains(t, lbs, v1alpha1.LoadBalancerDisabled)
	assert.Len(t, lbs, 3)
}

func TestValidCertManagers(t *testing.T) {
	t.Parallel()

	cms := v1alpha1.ValidCertManagers()

	assert.Contains(t, cms, v1alpha1.CertManagerEnabled)
	assert.Contains(t, cms, v1alpha1.CertManagerDisabled)
	assert.Len(t, cms, 2)
}

func TestValidPolicyEngines(t *testing.T) {
	t.Parallel()

	engines := v1alpha1.ValidPolicyEngines()

	assert.Contains(t, engines, v1alpha1.PolicyEngineNone)
	assert.Contains(t, engines, v1alpha1.PolicyEngineKyverno)
	assert.Contains(t, engines, v1alpha1.PolicyEngineGatekeeper)
	assert.Len(t, engines, 3)
}

func TestValidProviders(t *testing.T) {
	t.Parallel()

	providers := v1alpha1.ValidProviders()

	assert.Contains(t, providers, v1alpha1.ProviderDocker)
	assert.Contains(t, providers, v1alpha1.ProviderHetzner)
	assert.Len(t, providers, 2)
}

func TestValidPlacementGroupStrategies(t *testing.T) {
	t.Parallel()

	strategies := v1alpha1.ValidPlacementGroupStrategies()

	assert.Contains(t, strategies, v1alpha1.PlacementGroupStrategyNone)
	assert.Contains(t, strategies, v1alpha1.PlacementGroupStrategySpread)
	assert.Len(t, strategies, 2)
}

func TestValidateLocalRegistryForProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		provider  v1alpha1.Provider
		registry  v1alpha1.LocalRegistry
		wantError bool
	}{
		{
			"disabled registry is always valid",
			v1alpha1.ProviderHetzner,
			v1alpha1.LocalRegistry{},
			false,
		},
		{
			"Docker provider with local registry is valid",
			v1alpha1.ProviderDocker,
			v1alpha1.LocalRegistry{Registry: "localhost:5050"},
			false,
		},
		{
			"Docker provider with external registry is valid",
			v1alpha1.ProviderDocker,
			v1alpha1.LocalRegistry{Registry: "ghcr.io/myorg"},
			false,
		},
		{
			"Hetzner provider with external registry is valid",
			v1alpha1.ProviderHetzner,
			v1alpha1.LocalRegistry{Registry: "ghcr.io/myorg"},
			false,
		},
		{
			"Hetzner provider with local registry is invalid",
			v1alpha1.ProviderHetzner,
			v1alpha1.LocalRegistry{Registry: "localhost:5050"},
			true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := v1alpha1.ValidateLocalRegistryForProvider(testCase.provider, testCase.registry)

			if testCase.wantError {
				require.ErrorIs(t, err, v1alpha1.ErrLocalRegistryNotSupported)
			} else {
				require.NoError(t, err)
			}
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

//nolint:funlen // Table-driven test with comprehensive test cases.
func TestValidateMirrorRegistriesForProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		provider         v1alpha1.Provider
		mirrorRegistries []string
		wantError        bool
		errorContains    string
	}{
		{
			name:             "Docker: empty registries -> valid",
			provider:         v1alpha1.ProviderDocker,
			mirrorRegistries: []string{},
			wantError:        false,
		},
		{
			name:             "Docker: local mirror -> valid",
			provider:         v1alpha1.ProviderDocker,
			mirrorRegistries: []string{"docker.io=http://localhost:5000"},
			wantError:        false,
		},
		{
			name:             "Docker: external mirror -> valid",
			provider:         v1alpha1.ProviderDocker,
			mirrorRegistries: []string{"docker.io=https://mirror.gcr.io"},
			wantError:        false,
		},
		{
			name:             "Hetzner: empty registries -> valid",
			provider:         v1alpha1.ProviderHetzner,
			mirrorRegistries: []string{},
			wantError:        false,
		},
		{
			name:             "Hetzner: external mirror -> valid",
			provider:         v1alpha1.ProviderHetzner,
			mirrorRegistries: []string{"docker.io=https://mirror.gcr.io"},
			wantError:        false,
		},
		{
			name:     "Hetzner: multiple external mirrors -> valid",
			provider: v1alpha1.ProviderHetzner,
			mirrorRegistries: []string{
				"docker.io=https://mirror.gcr.io",
				"ghcr.io=https://ghcr.io",
			},
			wantError: false,
		},
		{
			name:             "Hetzner: localhost mirror -> error",
			provider:         v1alpha1.ProviderHetzner,
			mirrorRegistries: []string{"docker.io=http://localhost:5000"},
			wantError:        true,
			errorContains:    "local mirror registry not supported",
		},
		{
			name:             "Hetzner: 127.0.0.1 mirror -> error",
			provider:         v1alpha1.ProviderHetzner,
			mirrorRegistries: []string{"docker.io=http://127.0.0.1:5000"},
			wantError:        true,
			errorContains:    "local mirror registry not supported",
		},
		{
			name:             "Hetzner: host.docker.internal mirror -> error",
			provider:         v1alpha1.ProviderHetzner,
			mirrorRegistries: []string{"docker.io=http://host.docker.internal:5000"},
			wantError:        true,
			errorContains:    "local mirror registry not supported",
		},
		{
			name:     "Hetzner: mixed local and external -> error on local",
			provider: v1alpha1.ProviderHetzner,
			mirrorRegistries: []string{
				"docker.io=https://mirror.gcr.io",
				"ghcr.io=http://localhost:5000",
			},
			wantError:     true,
			errorContains: "local mirror registry not supported",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := v1alpha1.ValidateMirrorRegistriesForProvider(
				testCase.provider,
				testCase.mirrorRegistries,
			)
			if testCase.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.errorContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
