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
	assert.Len(t, values, 3)
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
	assert.Contains(t, values, "LocalPathStorage")
	assert.Len(t, values, 2)
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
