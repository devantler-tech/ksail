package v1alpha1_test

import (
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Default() and ValidValues() methods for all enum types.

func TestDistribution_Default(t *testing.T) {
	t.Parallel()

	var dist v1alpha1.Distribution
	assert.Equal(t, v1alpha1.DistributionVanilla, dist.Default())
}

func TestDistribution_ValidValues(t *testing.T) {
	t.Parallel()

	var dist v1alpha1.Distribution

	values := dist.ValidValues()
	assert.Contains(t, values, "Vanilla")
	assert.Contains(t, values, "K3s")
	assert.Contains(t, values, "Talos")
	assert.Contains(t, values, "VCluster")
	assert.Len(t, values, 4)
}

func TestCNI_Default(t *testing.T) {
	t.Parallel()

	var cni v1alpha1.CNI
	assert.Equal(t, v1alpha1.CNIDefault, cni.Default())
}

func TestCNI_ValidValues(t *testing.T) {
	t.Parallel()

	var cni v1alpha1.CNI

	values := cni.ValidValues()
	assert.Contains(t, values, "Default")
	assert.Contains(t, values, "Cilium")
	assert.Contains(t, values, "Calico")
	assert.Len(t, values, 3)
}

func TestCSI_Default(t *testing.T) {
	t.Parallel()

	var csi v1alpha1.CSI
	assert.Equal(t, v1alpha1.CSIDefault, csi.Default())
}

func TestCSI_ValidValues(t *testing.T) {
	t.Parallel()

	var csi v1alpha1.CSI

	values := csi.ValidValues()
	assert.Contains(t, values, "Default")
	assert.Contains(t, values, "Enabled")
	assert.Contains(t, values, "Disabled")
	assert.Len(t, values, 3)
}

func TestMetricsServer_Default(t *testing.T) {
	t.Parallel()

	var ms v1alpha1.MetricsServer
	assert.Equal(t, v1alpha1.MetricsServerDefault, ms.Default())
}

func TestMetricsServer_ValidValues(t *testing.T) {
	t.Parallel()

	var ms v1alpha1.MetricsServer

	values := ms.ValidValues()
	assert.Contains(t, values, "Default")
	assert.Contains(t, values, "Enabled")
	assert.Contains(t, values, "Disabled")
	assert.Len(t, values, 3)
}

func TestLoadBalancer_Set(t *testing.T) {
	t.Parallel()

	tests := getLoadBalancerSetTestCases()

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var loadBalancer v1alpha1.LoadBalancer

			err := loadBalancer.Set(testCase.input)
			if testCase.wantError {
				require.Error(t, err)
				require.ErrorIs(t, err, v1alpha1.ErrInvalidLoadBalancer)
			} else {
				require.NoError(t, err)
				assert.Equal(t, testCase.expected, loadBalancer)
			}
		})
	}
}

func getLoadBalancerSetTestCases() []struct {
	name      string
	input     string
	expected  v1alpha1.LoadBalancer
	wantError bool
} {
	return []struct {
		name      string
		input     string
		expected  v1alpha1.LoadBalancer
		wantError bool
	}{
		{
			name:      "default_lowercase",
			input:     "default",
			expected:  v1alpha1.LoadBalancerDefault,
			wantError: false,
		},
		{
			name:      "default_uppercase",
			input:     "DEFAULT",
			expected:  v1alpha1.LoadBalancerDefault,
			wantError: false,
		},
		{
			name:      "enabled_lowercase",
			input:     "enabled",
			expected:  v1alpha1.LoadBalancerEnabled,
			wantError: false,
		},
		{
			name:      "enabled_mixed_case",
			input:     "Enabled",
			expected:  v1alpha1.LoadBalancerEnabled,
			wantError: false,
		},
		{
			name:      "disabled_lowercase",
			input:     "disabled",
			expected:  v1alpha1.LoadBalancerDisabled,
			wantError: false,
		},
		{
			name:      "disabled_uppercase",
			input:     "DISABLED",
			expected:  v1alpha1.LoadBalancerDisabled,
			wantError: false,
		},
		{
			name:      "invalid_value",
			input:     "invalid",
			wantError: true,
		},
	}
}

func TestLoadBalancer_String(t *testing.T) {
	t.Parallel()

	lb := v1alpha1.LoadBalancerEnabled
	assert.Equal(t, "Enabled", lb.String())
}

func TestLoadBalancer_Type(t *testing.T) {
	t.Parallel()

	var lb v1alpha1.LoadBalancer
	assert.Equal(t, "LoadBalancer", lb.Type())
}

func TestLoadBalancer_Default(t *testing.T) {
	t.Parallel()

	var lb v1alpha1.LoadBalancer
	assert.Equal(t, v1alpha1.LoadBalancerDefault, lb.Default())
}

func TestLoadBalancer_ValidValues(t *testing.T) {
	t.Parallel()

	var lb v1alpha1.LoadBalancer

	values := lb.ValidValues()
	assert.Contains(t, values, "Default")
	assert.Contains(t, values, "Enabled")
	assert.Contains(t, values, "Disabled")
	assert.Len(t, values, 3)
}

func TestCertManager_Default(t *testing.T) {
	t.Parallel()

	var cm v1alpha1.CertManager
	assert.Equal(t, v1alpha1.CertManagerDisabled, cm.Default())
}

func TestCertManager_ValidValues(t *testing.T) {
	t.Parallel()

	var cm v1alpha1.CertManager

	values := cm.ValidValues()
	assert.Contains(t, values, "Enabled")
	assert.Contains(t, values, "Disabled")
	assert.Len(t, values, 2)
}

func TestPolicyEngine_Default(t *testing.T) {
	t.Parallel()

	var pe v1alpha1.PolicyEngine
	assert.Equal(t, v1alpha1.PolicyEngineNone, pe.Default())
}

func TestPolicyEngine_ValidValues(t *testing.T) {
	t.Parallel()

	var pe v1alpha1.PolicyEngine

	values := pe.ValidValues()
	assert.Contains(t, values, "None")
	assert.Contains(t, values, "Kyverno")
	assert.Contains(t, values, "Gatekeeper")
	assert.Len(t, values, 3)
}

func TestPolicyEngine_StringAndType(t *testing.T) {
	t.Parallel()

	pe := v1alpha1.PolicyEngineKyverno
	assert.Equal(t, "Kyverno", pe.String())
	assert.Equal(t, "PolicyEngine", pe.Type())
}

func TestGitOpsEngine_Default(t *testing.T) {
	t.Parallel()

	var engine v1alpha1.GitOpsEngine
	assert.Equal(t, v1alpha1.GitOpsEngineNone, engine.Default())
}

func TestGitOpsEngine_ValidValues(t *testing.T) {
	t.Parallel()

	var engine v1alpha1.GitOpsEngine

	values := engine.ValidValues()
	assert.Contains(t, values, "None")
	assert.Contains(t, values, "Flux")
	assert.Contains(t, values, "ArgoCD")
	assert.Len(t, values, 3)
}

// Provider tests.

func TestProvider_Set(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		expected  v1alpha1.Provider
		wantError bool
	}{
		{
			name:      "docker_lowercase",
			input:     "docker",
			expected:  v1alpha1.ProviderDocker,
			wantError: false,
		},
		{
			name:      "docker_uppercase",
			input:     "DOCKER",
			expected:  v1alpha1.ProviderDocker,
			wantError: false,
		},
		{
			name:      "hetzner_lowercase",
			input:     "hetzner",
			expected:  v1alpha1.ProviderHetzner,
			wantError: false,
		},
		{
			name:      "hetzner_mixed_case",
			input:     "Hetzner",
			expected:  v1alpha1.ProviderHetzner,
			wantError: false,
		},
		{
			name:      "invalid_provider",
			input:     "invalid",
			wantError: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var provider v1alpha1.Provider

			err := provider.Set(testCase.input)
			if testCase.wantError {
				require.Error(t, err)
				require.ErrorIs(t, err, v1alpha1.ErrInvalidProvider)
			} else {
				require.NoError(t, err)
				assert.Equal(t, testCase.expected, provider)
			}
		})
	}
}

func TestProvider_StringAndType(t *testing.T) {
	t.Parallel()

	provider := v1alpha1.ProviderDocker
	assert.Equal(t, "Docker", provider.String())
	assert.Equal(t, "Provider", provider.Type())
}

func TestProvider_Default(t *testing.T) {
	t.Parallel()

	var provider v1alpha1.Provider
	assert.Equal(t, v1alpha1.ProviderDocker, provider.Default())
}

func TestProvider_ValidValues(t *testing.T) {
	t.Parallel()

	var provider v1alpha1.Provider

	values := provider.ValidValues()
	assert.Contains(t, values, "Docker")
	assert.Contains(t, values, "Hetzner")
	assert.Len(t, values, 2)
}

func TestProvider_ValidateForDistribution_ValidCombinations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		provider     v1alpha1.Provider
		distribution v1alpha1.Distribution
	}{
		{"docker_for_vanilla", v1alpha1.ProviderDocker, v1alpha1.DistributionVanilla},
		{"docker_for_k3s", v1alpha1.ProviderDocker, v1alpha1.DistributionK3s},
		{"docker_for_talos", v1alpha1.ProviderDocker, v1alpha1.DistributionTalos},
		{"hetzner_for_talos", v1alpha1.ProviderHetzner, v1alpha1.DistributionTalos},
		{"empty_provider_defaults_to_docker", v1alpha1.Provider(""), v1alpha1.DistributionVanilla},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := testCase.provider.ValidateForDistribution(testCase.distribution)
			require.NoError(t, err)
		})
	}
}

func TestProvider_ValidateForDistribution_InvalidCombinations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		provider     v1alpha1.Provider
		distribution v1alpha1.Distribution
	}{
		{"hetzner_for_vanilla_invalid", v1alpha1.ProviderHetzner, v1alpha1.DistributionVanilla},
		{"hetzner_for_k3s_invalid", v1alpha1.ProviderHetzner, v1alpha1.DistributionK3s},
		{"unknown_distribution", v1alpha1.ProviderDocker, v1alpha1.Distribution("Unknown")},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := testCase.provider.ValidateForDistribution(testCase.distribution)
			require.Error(t, err)
		})
	}
}

func TestDistribution_ProvidesCSIByDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		provider     v1alpha1.Provider
		expected     bool
	}{
		{
			name:         "k3s_docker_provides_csi",
			distribution: v1alpha1.DistributionK3s,
			provider:     v1alpha1.ProviderDocker,
			expected:     true,
		},
		{
			name:         "vanilla_docker_no_csi",
			distribution: v1alpha1.DistributionVanilla,
			provider:     v1alpha1.ProviderDocker,
			expected:     false,
		},
		{
			name:         "talos_docker_no_csi",
			distribution: v1alpha1.DistributionTalos,
			provider:     v1alpha1.ProviderDocker,
			expected:     false,
		},
		{
			name:         "talos_hetzner_provides_csi",
			distribution: v1alpha1.DistributionTalos,
			provider:     v1alpha1.ProviderHetzner,
			expected:     true,
		},
		{
			name:         "vcluster_docker_provides_csi",
			distribution: v1alpha1.DistributionVCluster,
			provider:     v1alpha1.ProviderDocker,
			expected:     true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := testCase.distribution.ProvidesCSIByDefault(testCase.provider)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestDistribution_ProvidesLoadBalancerByDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		provider     v1alpha1.Provider
		expected     bool
	}{
		{
			name:         "k3s_docker_provides_loadbalancer",
			distribution: v1alpha1.DistributionK3s,
			provider:     v1alpha1.ProviderDocker,
			expected:     true,
		},
		{
			name:         "vanilla_docker_no_loadbalancer",
			distribution: v1alpha1.DistributionVanilla,
			provider:     v1alpha1.ProviderDocker,
			expected:     false,
		},
		{
			name:         "talos_docker_no_loadbalancer",
			distribution: v1alpha1.DistributionTalos,
			provider:     v1alpha1.ProviderDocker,
			expected:     false,
		},
		{
			name:         "talos_hetzner_provides_loadbalancer",
			distribution: v1alpha1.DistributionTalos,
			provider:     v1alpha1.ProviderHetzner,
			expected:     true,
		},
		{
			name:         "vcluster_docker_provides_loadbalancer",
			distribution: v1alpha1.DistributionVCluster,
			provider:     v1alpha1.ProviderDocker,
			expected:     true,
		},
		{
			name:         "unknown_distribution_no_loadbalancer",
			distribution: v1alpha1.Distribution("Unknown"),
			provider:     v1alpha1.ProviderDocker,
			expected:     false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := testCase.distribution.ProvidesLoadBalancerByDefault(testCase.provider)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

// DefaultClusterName tests.

func TestDistribution_DefaultClusterName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		expected     string
	}{
		{
			name:         "vanilla_returns_kind",
			distribution: v1alpha1.DistributionVanilla,
			expected:     "kind",
		},
		{
			name:         "k3s_returns_k3d_default",
			distribution: v1alpha1.DistributionK3s,
			expected:     "k3d-default",
		},
		{
			name:         "talos_returns_talos_default",
			distribution: v1alpha1.DistributionTalos,
			expected:     "talos-default",
		},
		{
			name:         "unknown_returns_kind",
			distribution: v1alpha1.Distribution("Unknown"),
			expected:     "kind",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := testCase.distribution.DefaultClusterName()
			assert.Equal(t, testCase.expected, result)
		})
	}
}

// PlacementGroupStrategy tests.

//nolint:funlen // Table-driven test with comprehensive test cases.
func TestPlacementGroupStrategy_Set(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		expected  v1alpha1.PlacementGroupStrategy
		wantError bool
	}{
		{
			name:     "none_lowercase",
			input:    "none",
			expected: v1alpha1.PlacementGroupStrategyNone,
		},
		{
			name:     "none_uppercase",
			input:    "NONE",
			expected: v1alpha1.PlacementGroupStrategyNone,
		},
		{
			name:     "none_mixed_case",
			input:    "None",
			expected: v1alpha1.PlacementGroupStrategyNone,
		},
		{
			name:     "spread_lowercase",
			input:    "spread",
			expected: v1alpha1.PlacementGroupStrategySpread,
		},
		{
			name:     "spread_uppercase",
			input:    "SPREAD",
			expected: v1alpha1.PlacementGroupStrategySpread,
		},
		{
			name:     "spread_mixed_case",
			input:    "Spread",
			expected: v1alpha1.PlacementGroupStrategySpread,
		},
		{
			name:      "invalid_value",
			input:     "invalid",
			wantError: true,
		},
		{
			name:      "empty_string",
			input:     "",
			wantError: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var strategy v1alpha1.PlacementGroupStrategy

			err := strategy.Set(testCase.input)
			if testCase.wantError {
				require.Error(t, err)
				require.ErrorIs(t, err, v1alpha1.ErrInvalidPlacementGroupStrategy)
			} else {
				require.NoError(t, err)
				assert.Equal(t, testCase.expected, strategy)
			}
		})
	}
}

func TestPlacementGroupStrategy_StringAndType(t *testing.T) {
	t.Parallel()

	strategy := v1alpha1.PlacementGroupStrategySpread
	assert.Equal(t, "Spread", strategy.String())
	assert.Equal(t, "PlacementGroupStrategy", strategy.Type())

	none := v1alpha1.PlacementGroupStrategyNone
	assert.Equal(t, "None", none.String())
}

func TestPlacementGroupStrategy_ValidValues(t *testing.T) {
	t.Parallel()

	var strategy v1alpha1.PlacementGroupStrategy

	values := strategy.ValidValues()
	assert.Contains(t, values, "None")
	assert.Contains(t, values, "Spread")
	assert.Len(t, values, 2)
}

// Defaults tests.

func TestExpectedDistributionConfigName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		expected     string
	}{
		{
			name:         "vanilla_returns_kind_yaml",
			distribution: v1alpha1.DistributionVanilla,
			expected:     "kind.yaml",
		},
		{
			name:         "k3s_returns_k3d_yaml",
			distribution: v1alpha1.DistributionK3s,
			expected:     "k3d.yaml",
		},
		{
			name:         "talos_returns_talos",
			distribution: v1alpha1.DistributionTalos,
			expected:     "talos",
		},
		{
			name:         "unknown_defaults_to_kind_yaml",
			distribution: v1alpha1.Distribution("Unknown"),
			expected:     "kind.yaml",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := v1alpha1.ExpectedDistributionConfigName(testCase.distribution)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestExpectedContextName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		expected     string
	}{
		{
			name:         "vanilla_returns_kind_context",
			distribution: v1alpha1.DistributionVanilla,
			expected:     "kind-kind",
		},
		{
			name:         "k3s_returns_k3d_context",
			distribution: v1alpha1.DistributionK3s,
			expected:     "k3d-k3d-default",
		},
		{
			name:         "talos_returns_admin_context",
			distribution: v1alpha1.DistributionTalos,
			expected:     "admin@talos-default",
		},
		{
			name:         "unknown_returns_empty",
			distribution: v1alpha1.Distribution("Unknown"),
			expected:     "",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := v1alpha1.ExpectedContextName(testCase.distribution)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

// EffectiveValue tests.

//nolint:funlen // Table-driven test with comprehensive distribution × value combinations.
func TestCSI_EffectiveValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		csi          v1alpha1.CSI
		distribution v1alpha1.Distribution
		provider     v1alpha1.Provider
		expected     v1alpha1.CSI
	}{
		{
			name:         "vanilla_docker_default_resolves_to_disabled",
			csi:          v1alpha1.CSIDefault,
			distribution: v1alpha1.DistributionVanilla,
			provider:     v1alpha1.ProviderDocker,
			expected:     v1alpha1.CSIDisabled,
		},
		{
			name:         "k3s_docker_default_resolves_to_enabled",
			csi:          v1alpha1.CSIDefault,
			distribution: v1alpha1.DistributionK3s,
			provider:     v1alpha1.ProviderDocker,
			expected:     v1alpha1.CSIEnabled,
		},
		{
			name:         "talos_docker_default_resolves_to_disabled",
			csi:          v1alpha1.CSIDefault,
			distribution: v1alpha1.DistributionTalos,
			provider:     v1alpha1.ProviderDocker,
			expected:     v1alpha1.CSIDisabled,
		},
		{
			name:         "talos_hetzner_default_resolves_to_enabled",
			csi:          v1alpha1.CSIDefault,
			distribution: v1alpha1.DistributionTalos,
			provider:     v1alpha1.ProviderHetzner,
			expected:     v1alpha1.CSIEnabled,
		},
		{
			name:         "enabled_passes_through",
			csi:          v1alpha1.CSIEnabled,
			distribution: v1alpha1.DistributionVanilla,
			provider:     v1alpha1.ProviderDocker,
			expected:     v1alpha1.CSIEnabled,
		},
		{
			name:         "disabled_passes_through",
			csi:          v1alpha1.CSIDisabled,
			distribution: v1alpha1.DistributionK3s,
			provider:     v1alpha1.ProviderDocker,
			expected:     v1alpha1.CSIDisabled,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := testCase.csi.EffectiveValue(testCase.distribution, testCase.provider)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestMetricsServer_EffectiveValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		ms           v1alpha1.MetricsServer
		distribution v1alpha1.Distribution
		expected     v1alpha1.MetricsServer
	}{
		{
			name:         "vanilla_default_resolves_to_disabled",
			ms:           v1alpha1.MetricsServerDefault,
			distribution: v1alpha1.DistributionVanilla,
			expected:     v1alpha1.MetricsServerDisabled,
		},
		{
			name:         "k3s_default_resolves_to_enabled",
			ms:           v1alpha1.MetricsServerDefault,
			distribution: v1alpha1.DistributionK3s,
			expected:     v1alpha1.MetricsServerEnabled,
		},
		{
			name:         "talos_default_resolves_to_disabled",
			ms:           v1alpha1.MetricsServerDefault,
			distribution: v1alpha1.DistributionTalos,
			expected:     v1alpha1.MetricsServerDisabled,
		},
		{
			name:         "enabled_passes_through",
			ms:           v1alpha1.MetricsServerEnabled,
			distribution: v1alpha1.DistributionVanilla,
			expected:     v1alpha1.MetricsServerEnabled,
		},
		{
			name:         "disabled_passes_through",
			ms:           v1alpha1.MetricsServerDisabled,
			distribution: v1alpha1.DistributionK3s,
			expected:     v1alpha1.MetricsServerDisabled,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := testCase.ms.EffectiveValue(testCase.distribution)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

//nolint:funlen // Table-driven test with comprehensive distribution × value combinations.
func TestLoadBalancer_EffectiveValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		lb           v1alpha1.LoadBalancer
		distribution v1alpha1.Distribution
		provider     v1alpha1.Provider
		expected     v1alpha1.LoadBalancer
	}{
		{
			name:         "vanilla_docker_default_resolves_to_disabled",
			lb:           v1alpha1.LoadBalancerDefault,
			distribution: v1alpha1.DistributionVanilla,
			provider:     v1alpha1.ProviderDocker,
			expected:     v1alpha1.LoadBalancerDisabled,
		},
		{
			name:         "k3s_docker_default_resolves_to_enabled",
			lb:           v1alpha1.LoadBalancerDefault,
			distribution: v1alpha1.DistributionK3s,
			provider:     v1alpha1.ProviderDocker,
			expected:     v1alpha1.LoadBalancerEnabled,
		},
		{
			name:         "talos_docker_default_resolves_to_disabled",
			lb:           v1alpha1.LoadBalancerDefault,
			distribution: v1alpha1.DistributionTalos,
			provider:     v1alpha1.ProviderDocker,
			expected:     v1alpha1.LoadBalancerDisabled,
		},
		{
			name:         "talos_docker_enabled_passes_through",
			lb:           v1alpha1.LoadBalancerEnabled,
			distribution: v1alpha1.DistributionTalos,
			provider:     v1alpha1.ProviderDocker,
			expected:     v1alpha1.LoadBalancerEnabled,
		},
		{
			name:         "talos_docker_disabled_resolves_to_disabled",
			lb:           v1alpha1.LoadBalancerDisabled,
			distribution: v1alpha1.DistributionTalos,
			provider:     v1alpha1.ProviderDocker,
			expected:     v1alpha1.LoadBalancerDisabled,
		},
		{
			name:         "talos_hetzner_default_resolves_to_enabled",
			lb:           v1alpha1.LoadBalancerDefault,
			distribution: v1alpha1.DistributionTalos,
			provider:     v1alpha1.ProviderHetzner,
			expected:     v1alpha1.LoadBalancerEnabled,
		},
		{
			name:         "enabled_passes_through",
			lb:           v1alpha1.LoadBalancerEnabled,
			distribution: v1alpha1.DistributionVanilla,
			provider:     v1alpha1.ProviderDocker,
			expected:     v1alpha1.LoadBalancerEnabled,
		},
		{
			name:         "disabled_passes_through",
			lb:           v1alpha1.LoadBalancerDisabled,
			distribution: v1alpha1.DistributionK3s,
			provider:     v1alpha1.ProviderDocker,
			expected:     v1alpha1.LoadBalancerDisabled,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := testCase.lb.EffectiveValue(testCase.distribution, testCase.provider)
			assert.Equal(t, testCase.expected, result)
		})
	}
}
