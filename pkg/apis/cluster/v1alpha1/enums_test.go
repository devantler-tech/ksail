package v1alpha1_test

import (
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
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
	assert.Contains(t, values, "KWOK")
	assert.Contains(t, values, "EKS")
	assert.Len(t, values, 6)
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

//nolint:funlen // Table-driven test with multiple provider cases is clearer as single function
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
			name:      "omni_lowercase",
			input:     "omni",
			expected:  v1alpha1.ProviderOmni,
			wantError: false,
		},
		{
			name:      "kubernetes_lowercase",
			input:     "kubernetes",
			expected:  v1alpha1.ProviderKubernetes,
			wantError: false,
		},
		{
			name:      "kubernetes_mixed_case",
			input:     "Kubernetes",
			expected:  v1alpha1.ProviderKubernetes,
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
	assert.Contains(t, values, "Omni")
	assert.Contains(t, values, "AWS")
	assert.Contains(t, values, "Kubernetes")
	assert.Len(t, values, 5)
}

func TestProvider_IsCloud(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider v1alpha1.Provider
		expected bool
	}{
		{"docker", v1alpha1.ProviderDocker, false},
		{"hetzner", v1alpha1.ProviderHetzner, true},
		{"omni", v1alpha1.ProviderOmni, true},
		{"aws", v1alpha1.ProviderAWS, true},
		{"kubernetes", v1alpha1.ProviderKubernetes, false},
		{"empty", v1alpha1.Provider(""), false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.expected, testCase.provider.IsCloud())
		})
	}
}

func TestDefaultProviderForDistribution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		want         v1alpha1.Provider
	}{
		{"vanilla_defaults_docker", v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker},
		{"k3s_defaults_docker", v1alpha1.DistributionK3s, v1alpha1.ProviderDocker},
		{"talos_defaults_docker", v1alpha1.DistributionTalos, v1alpha1.ProviderDocker},
		{"vcluster_defaults_docker", v1alpha1.DistributionVCluster, v1alpha1.ProviderDocker},
		{"kwok_defaults_docker", v1alpha1.DistributionKWOK, v1alpha1.ProviderDocker},
		{"eks_defaults_aws", v1alpha1.DistributionEKS, v1alpha1.ProviderAWS},
		{"unknown_defaults_empty", v1alpha1.Distribution("Bogus"), v1alpha1.Provider("")},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(
				t,
				testCase.want,
				v1alpha1.DefaultProviderForDistribution(testCase.distribution),
			)
		})
	}
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
		{"omni_for_talos", v1alpha1.ProviderOmni, v1alpha1.DistributionTalos},
		{"kubernetes_for_vanilla", v1alpha1.ProviderKubernetes, v1alpha1.DistributionVanilla},
		{"kubernetes_for_k3s", v1alpha1.ProviderKubernetes, v1alpha1.DistributionK3s},
		{"kubernetes_for_talos", v1alpha1.ProviderKubernetes, v1alpha1.DistributionTalos},
		{"kubernetes_for_vcluster", v1alpha1.ProviderKubernetes, v1alpha1.DistributionVCluster},
		{"kubernetes_for_kwok", v1alpha1.ProviderKubernetes, v1alpha1.DistributionKWOK},
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
		{"omni_for_vanilla_invalid", v1alpha1.ProviderOmni, v1alpha1.DistributionVanilla},
		{"omni_for_k3s_invalid", v1alpha1.ProviderOmni, v1alpha1.DistributionK3s},
		{"kubernetes_for_eks_invalid", v1alpha1.ProviderKubernetes, v1alpha1.DistributionEKS},
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
			name:         "vcluster_docker_no_csi",
			distribution: v1alpha1.DistributionVCluster,
			provider:     v1alpha1.ProviderDocker,
			expected:     false,
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

func TestCSI_EffectiveValue(t *testing.T) { //nolint:dupl,funlen // enum pattern
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
		{
			name:         "empty_string_treated_as_default",
			csi:          v1alpha1.CSI(""),
			distribution: v1alpha1.DistributionK3s,
			provider:     v1alpha1.ProviderDocker,
			expected:     v1alpha1.CSIEnabled,
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
		{
			name:         "empty_string_treated_as_default",
			ms:           v1alpha1.MetricsServer(""),
			distribution: v1alpha1.DistributionK3s,
			expected:     v1alpha1.MetricsServerEnabled,
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
		{
			name:         "empty_string_treated_as_default",
			lb:           v1alpha1.LoadBalancer(""),
			distribution: v1alpha1.DistributionK3s,
			provider:     v1alpha1.ProviderDocker,
			expected:     v1alpha1.LoadBalancerEnabled,
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

func TestImageVerification_Default(t *testing.T) {
	t.Parallel()

	var imageVerification v1alpha1.ImageVerification
	assert.Equal(t, v1alpha1.ImageVerificationDisabled, imageVerification.Default())
}

func TestImageVerification_ValidValues(t *testing.T) {
	t.Parallel()

	var imageVerification v1alpha1.ImageVerification

	values := imageVerification.ValidValues()
	assert.Contains(t, values, "Enabled")
	assert.Contains(t, values, "Disabled")
	assert.Len(t, values, 2)
}

func TestImageVerification_Set(t *testing.T) { //nolint:dupl // enum pattern
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		expected  v1alpha1.ImageVerification
		wantError bool
	}{
		{name: "enabled_lowercase", input: "enabled", expected: v1alpha1.ImageVerificationEnabled},
		{name: "enabled_mixed_case", input: "Enabled", expected: v1alpha1.ImageVerificationEnabled},
		{name: "enabled_uppercase", input: "ENABLED", expected: v1alpha1.ImageVerificationEnabled},
		{
			name:     "disabled_lowercase",
			input:    "disabled",
			expected: v1alpha1.ImageVerificationDisabled,
		},
		{
			name:     "disabled_mixed_case",
			input:    "Disabled",
			expected: v1alpha1.ImageVerificationDisabled,
		},
		{
			name:     "disabled_uppercase",
			input:    "DISABLED",
			expected: v1alpha1.ImageVerificationDisabled,
		},
		{name: "invalid_value", input: "invalid", wantError: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var imageVerification v1alpha1.ImageVerification

			err := imageVerification.Set(testCase.input)
			if testCase.wantError {
				require.Error(t, err)
				require.ErrorIs(t, err, v1alpha1.ErrInvalidImageVerification)
			} else {
				require.NoError(t, err)
				assert.Equal(t, testCase.expected, imageVerification)
			}
		})
	}
}

func TestImageVerification_StringAndType(t *testing.T) {
	t.Parallel()

	imageVerification := v1alpha1.ImageVerificationEnabled
	assert.Equal(t, "Enabled", imageVerification.String())
	assert.Equal(t, "ImageVerification", imageVerification.Type())
}

func TestCDI_Default(t *testing.T) {
	t.Parallel()

	var cdi v1alpha1.CDI
	assert.Equal(t, v1alpha1.CDIDefault, cdi.Default())
}

func TestCDI_ValidValues(t *testing.T) {
	t.Parallel()

	var cdi v1alpha1.CDI

	values := cdi.ValidValues()
	assert.Contains(t, values, "Default")
	assert.Contains(t, values, "Enabled")
	assert.Contains(t, values, "Disabled")
	assert.Len(t, values, 3)
}

func TestCDI_StringAndType(t *testing.T) {
	t.Parallel()

	cdi := v1alpha1.CDIEnabled
	assert.Equal(t, "Enabled", cdi.String())
	assert.Equal(t, "CDI", cdi.Type())
}

func TestDistribution_ProvidesCDIByDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		expected     bool
	}{
		{
			name:         "talos_provides_cdi",
			distribution: v1alpha1.DistributionTalos,
			expected:     true,
		},
		{
			name:         "vanilla_no_cdi",
			distribution: v1alpha1.DistributionVanilla,
			expected:     false,
		},
		{
			name:         "k3s_no_cdi",
			distribution: v1alpha1.DistributionK3s,
			expected:     false,
		},
		{
			name:         "vcluster_no_cdi",
			distribution: v1alpha1.DistributionVCluster,
			expected:     false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := testCase.distribution.ProvidesCDIByDefault()
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestCDI_EffectiveValue(t *testing.T) { //nolint:dupl,funlen // enum pattern
	t.Parallel()

	tests := []struct {
		name         string
		cdi          v1alpha1.CDI
		distribution v1alpha1.Distribution
		provider     v1alpha1.Provider
		expected     v1alpha1.CDI
	}{
		{
			name:         "talos_docker_default_resolves_to_enabled",
			cdi:          v1alpha1.CDIDefault,
			distribution: v1alpha1.DistributionTalos,
			provider:     v1alpha1.ProviderDocker,
			expected:     v1alpha1.CDIEnabled,
		},
		{
			name:         "vanilla_docker_default_resolves_to_disabled",
			cdi:          v1alpha1.CDIDefault,
			distribution: v1alpha1.DistributionVanilla,
			provider:     v1alpha1.ProviderDocker,
			expected:     v1alpha1.CDIDisabled,
		},
		{
			name:         "k3s_docker_default_resolves_to_disabled",
			cdi:          v1alpha1.CDIDefault,
			distribution: v1alpha1.DistributionK3s,
			provider:     v1alpha1.ProviderDocker,
			expected:     v1alpha1.CDIDisabled,
		},
		{
			name:         "vcluster_docker_default_resolves_to_disabled",
			cdi:          v1alpha1.CDIDefault,
			distribution: v1alpha1.DistributionVCluster,
			provider:     v1alpha1.ProviderDocker,
			expected:     v1alpha1.CDIDisabled,
		},
		{
			name:         "enabled_passes_through",
			cdi:          v1alpha1.CDIEnabled,
			distribution: v1alpha1.DistributionVanilla,
			provider:     v1alpha1.ProviderDocker,
			expected:     v1alpha1.CDIEnabled,
		},
		{
			name:         "disabled_passes_through",
			cdi:          v1alpha1.CDIDisabled,
			distribution: v1alpha1.DistributionTalos,
			provider:     v1alpha1.ProviderDocker,
			expected:     v1alpha1.CDIDisabled,
		},
		{
			name:         "empty_string_treated_as_default",
			cdi:          v1alpha1.CDI(""),
			distribution: v1alpha1.DistributionTalos,
			provider:     v1alpha1.ProviderDocker,
			expected:     v1alpha1.CDIEnabled,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := testCase.cdi.EffectiveValue(testCase.distribution, testCase.provider)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestIngressFirewall_Default(t *testing.T) {
	t.Parallel()

	var ingressFirewall v1alpha1.IngressFirewall
	assert.Equal(t, v1alpha1.IngressFirewallEnabled, ingressFirewall.Default())
}

func TestIngressFirewall_ValidValues(t *testing.T) {
	t.Parallel()

	var ingressFirewall v1alpha1.IngressFirewall

	values := ingressFirewall.ValidValues()
	assert.Contains(t, values, "Enabled")
	assert.Contains(t, values, "Disabled")
	assert.Len(t, values, 2)
}

func TestIngressFirewall_Set(t *testing.T) { //nolint:dupl // enum pattern
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		expected  v1alpha1.IngressFirewall
		wantError bool
	}{
		{name: "enabled_lowercase", input: "enabled", expected: v1alpha1.IngressFirewallEnabled},
		{name: "enabled_mixed_case", input: "Enabled", expected: v1alpha1.IngressFirewallEnabled},
		{name: "enabled_uppercase", input: "ENABLED", expected: v1alpha1.IngressFirewallEnabled},
		{name: "disabled_lowercase", input: "disabled", expected: v1alpha1.IngressFirewallDisabled},
		{
			name:     "disabled_mixed_case",
			input:    "Disabled",
			expected: v1alpha1.IngressFirewallDisabled,
		},
		{name: "disabled_uppercase", input: "DISABLED", expected: v1alpha1.IngressFirewallDisabled},
		{name: "invalid_value", input: "invalid", wantError: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var ingressFirewall v1alpha1.IngressFirewall

			err := ingressFirewall.Set(testCase.input)
			if testCase.wantError {
				require.Error(t, err)
				require.ErrorIs(t, err, v1alpha1.ErrInvalidIngressFirewall)
			} else {
				require.NoError(t, err)
				assert.Equal(t, testCase.expected, ingressFirewall)
			}
		})
	}
}

func TestIngressFirewall_StringAndType(t *testing.T) {
	t.Parallel()

	ingressFirewall := v1alpha1.IngressFirewallEnabled
	assert.Equal(t, "Enabled", ingressFirewall.String())
	assert.Equal(t, "IngressFirewall", ingressFirewall.Type())
}

// Set(), IsValid(), and String()/Type() tests below were migrated from
// constructors_test.go when the zero-value constructor chain was deleted.

// defaultEnumValue is the canonical "Default" enum value shared by the
// CNI, CSI, and MetricsServer Set() tests below.
const defaultEnumValue = "Default"

func TestDistribution_Set(t *testing.T) {
	t.Parallel()

	validCases := []struct{ input, expected string }{
		{"Vanilla", "Vanilla"},
		{"k3s", "K3s"},
	}
	for _, validCase := range validCases {
		var dist v1alpha1.Distribution

		require.NoError(t, dist.Set(validCase.input))
	}

	err := func() error {
		var dist v1alpha1.Distribution

		return dist.Set("invalid")
	}()
	assertErrWrappedContains(
		t,
		err,
		v1alpha1.ErrInvalidDistribution,
		"invalid",
		"Set(invalid)",
	)
}

func TestDistribution_IsValid(t *testing.T) {
	t.Parallel()

	validCases := []v1alpha1.Distribution{
		v1alpha1.DistributionVanilla,
		v1alpha1.DistributionK3s,
	}

	for _, dist := range validCases {
		assert.True(t, dist.IsValid(), "Distribution %s should be valid", dist)
	}

	invalidCases := []v1alpha1.Distribution{
		"",
		"invalid",
		"docker",
		"kubernetes",
	}

	for _, dist := range invalidCases {
		assert.False(t, dist.IsValid(), "Distribution %s should be invalid", dist)
	}
}

func TestGitOpsEngine_Set(t *testing.T) {
	t.Parallel()

	validCases := []struct {
		name     string
		input    string
		expected v1alpha1.GitOpsEngine
	}{
		{name: "legacy none", input: "None", expected: v1alpha1.GitOpsEngineNone},
		{name: "mixed case none", input: "nOnE", expected: v1alpha1.GitOpsEngineNone},
		{name: "flux", input: "Flux", expected: v1alpha1.GitOpsEngineFlux},
		{name: "flux lowercase", input: "flux", expected: v1alpha1.GitOpsEngineFlux},
	}

	for _, testCase := range validCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var tool v1alpha1.GitOpsEngine
			require.NoError(t, tool.Set(testCase.input))
			assert.Equal(t, testCase.expected, tool)
		})
	}

	err := func() error {
		var tool v1alpha1.GitOpsEngine

		return tool.Set("invalid")
	}()
	assertErrWrappedContains(
		t,
		err,
		v1alpha1.ErrInvalidGitOpsEngine,
		"invalid",
		"Set(invalid)",
	)
}

func TestCNI_Set(t *testing.T) {
	t.Parallel()

	validCases := []struct{ input, expected string }{
		{defaultEnumValue, defaultEnumValue},
		{"cilium", "Cilium"},
		{"CILIUM", "Cilium"},
	}
	for _, validCase := range validCases {
		var cni v1alpha1.CNI

		require.NoError(t, cni.Set(validCase.input))
	}

	err := func() error {
		var cni v1alpha1.CNI

		return cni.Set("invalid")
	}()
	assertErrWrappedContains(
		t,
		err,
		v1alpha1.ErrInvalidCNI,
		"invalid",
		"Set(invalid)",
	)
}

func TestCSI_Set(t *testing.T) {
	t.Parallel()

	validCases := []struct{ input, expected string }{
		{defaultEnumValue, defaultEnumValue},
		{"enabled", "Enabled"},
		{"ENABLED", "Enabled"},
		{"disabled", "Disabled"},
		{"DISABLED", "Disabled"},
	}
	for _, validCase := range validCases {
		var csi v1alpha1.CSI

		require.NoError(t, csi.Set(validCase.input))
	}

	err := func() error {
		var csi v1alpha1.CSI

		return csi.Set("invalid")
	}()
	assertErrWrappedContains(
		t,
		err,
		v1alpha1.ErrInvalidCSI,
		"invalid",
		"Set(invalid)",
	)
}

func TestMetricsServer_Set(t *testing.T) {
	t.Parallel()

	validCases := []struct{ input, expected string }{
		{defaultEnumValue, defaultEnumValue},
		{"default", defaultEnumValue},
		{"DEFAULT", defaultEnumValue},
		{"Enabled", "Enabled"},
		{"enabled", "Enabled"},
		{"ENABLED", "Enabled"},
		{"Disabled", "Disabled"},
		{"disabled", "Disabled"},
		{"DISABLED", "Disabled"},
	}
	for _, validCase := range validCases {
		var ms v1alpha1.MetricsServer

		require.NoError(t, ms.Set(validCase.input))
		assert.Equal(t, validCase.expected, string(ms))
	}

	err := func() error {
		var ms v1alpha1.MetricsServer

		return ms.Set("invalid")
	}()
	assertErrWrappedContains(
		t,
		err,
		v1alpha1.ErrInvalidMetricsServer,
		"invalid",
		"Set(invalid)",
	)
}

func TestCertManager_Set(t *testing.T) {
	t.Parallel()

	validCases := []struct{ input, expected string }{
		{"Enabled", "Enabled"},
		{"enabled", "Enabled"},
		{"ENABLED", "Enabled"},
		{"Disabled", "Disabled"},
		{"disabled", "Disabled"},
		{"DISABLED", "Disabled"},
	}
	for _, validCase := range validCases {
		var cm v1alpha1.CertManager

		require.NoError(t, cm.Set(validCase.input))
		assert.Equal(t, validCase.expected, string(cm))
	}

	err := func() error {
		var cm v1alpha1.CertManager

		return cm.Set("invalid")
	}()
	assertErrWrappedContains(
		t,
		err,
		v1alpha1.ErrInvalidCertManager,
		"invalid",
		"Set(invalid)",
	)
}

//nolint:unparam // contains always receives "invalid" which is intentional for Set() error tests
func assertErrWrappedContains(t *testing.T, got error, want error, contains string, ctx string) {
	t.Helper()

	if want != nil {
		require.ErrorIs(t, got, want, ctx)
	} else {
		require.Error(t, got, ctx)
	}

	if contains != "" {
		assert.ErrorContains(t, got, contains, ctx)
	}
}

func TestStringAndTypeMethods(t *testing.T) {
	t.Parallel()

	// Test String() and Type() methods for pflags interface
	dist := v1alpha1.DistributionVanilla
	assert.Equal(t, "Vanilla", dist.String())
	assert.Equal(t, "Distribution", dist.Type())

	tool := v1alpha1.GitOpsEngineNone
	assert.Equal(t, "None", tool.String())
	assert.Equal(t, "GitOpsEngine", tool.Type())

	cni := v1alpha1.CNIDefault
	assert.Equal(t, defaultEnumValue, cni.String())
	assert.Equal(t, "CNI", cni.Type())

	csi := v1alpha1.CSIDefault
	assert.Equal(t, defaultEnumValue, csi.String())
	assert.Equal(t, "CSI", csi.Type())

	ms := v1alpha1.MetricsServerEnabled
	assert.Equal(t, "Enabled", ms.String())
	assert.Equal(t, "MetricsServer", ms.Type())

	msDisabled := v1alpha1.MetricsServerDisabled
	assert.Equal(t, "Disabled", msDisabled.String())
	assert.Equal(t, "MetricsServer", msDisabled.Type())

	cm := v1alpha1.CertManagerDisabled
	assert.Equal(t, "Disabled", cm.String())
	assert.Equal(t, "CertManager", cm.Type())
}
