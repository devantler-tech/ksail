package v1alpha1_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Registry-driven enum conformance suite.
//
// Every string-based enum implementing the pflag.Value + EnumValuer contract
// is registered once in enumSpecs(). The conformance test then covers, for
// each enum: exact ValidValues(), Type(), Default(), case-insensitive Set()
// round-trips for every value, and invalid/empty Set() errors (sentinel
// wrapping plus an error message listing the input and all valid options).
// ---------------------------------------------------------------------------

// Canonical enum value spellings shared across the registry below.
const (
	valueNone     = "None"
	valueDefault  = "Default"
	valueEnabled  = "Enabled"
	valueDisabled = "Disabled"
)

// enumValue is the contract shared by all full KSail enum types.
type enumValue interface {
	Set(value string) error
	String() string
	Type() string
	Default() any
	ValidValues() []string
}

// enumSpec registers one enum type for the conformance suite.
type enumSpec struct {
	// typeName is the expected Type() string (also the subtest name).
	typeName string
	// newValue returns a fresh zero value of the enum.
	newValue func() enumValue
	// values are the expected ValidValues(), in canonical order.
	values []string
	// defaultsTo is the expected (typed) Default() value.
	defaultsTo any
	// invalidErr is the sentinel wrapped by Set() on invalid input.
	invalidErr error
}

// enumSpecs returns the conformance registry for all full enum types.
func enumSpecs() []enumSpec {
	return slices.Concat(
		clusterEnumSpecs(),
		componentEnumSpecs(),
		policyEnumSpecs(),
		autoscalerEnumSpecs(),
	)
}

// clusterEnumSpecs registers the cluster-shape enums (distribution, provider,
// GitOps engine, and Hetzner placement strategy).
func clusterEnumSpecs() []enumSpec {
	return []enumSpec{
		{
			typeName:   "Distribution",
			newValue:   func() enumValue { return new(v1alpha1.Distribution) },
			values:     []string{"Vanilla", "K3s", "Talos", "VCluster", "KWOK", "EKS"},
			defaultsTo: v1alpha1.DistributionVanilla,
			invalidErr: v1alpha1.ErrInvalidDistribution,
		},
		{
			typeName:   "Provider",
			newValue:   func() enumValue { return new(v1alpha1.Provider) },
			values:     []string{"Docker", "Hetzner", "Omni", "AWS", "Kubernetes"},
			defaultsTo: v1alpha1.ProviderDocker,
			invalidErr: v1alpha1.ErrInvalidProvider,
		},
		{
			typeName:   "GitOpsEngine",
			newValue:   func() enumValue { return new(v1alpha1.GitOpsEngine) },
			values:     []string{valueNone, "Flux", "ArgoCD"},
			defaultsTo: v1alpha1.GitOpsEngineNone,
			invalidErr: v1alpha1.ErrInvalidGitOpsEngine,
		},
		{
			typeName:   "PlacementGroupStrategy",
			newValue:   func() enumValue { return new(v1alpha1.PlacementGroupStrategy) },
			values:     []string{valueNone, "Spread"},
			defaultsTo: v1alpha1.PlacementGroupStrategySpread,
			invalidErr: v1alpha1.ErrInvalidPlacementGroupStrategy,
		},
	}
}

// componentEnumSpecs registers the cluster component toggle enums.
func componentEnumSpecs() []enumSpec {
	return []enumSpec{
		{
			typeName:   "CNI",
			newValue:   func() enumValue { return new(v1alpha1.CNI) },
			values:     []string{valueDefault, "Cilium", "Calico"},
			defaultsTo: v1alpha1.CNIDefault,
			invalidErr: v1alpha1.ErrInvalidCNI,
		},
		{
			typeName:   "CSI",
			newValue:   func() enumValue { return new(v1alpha1.CSI) },
			values:     []string{valueDefault, valueEnabled, valueDisabled},
			defaultsTo: v1alpha1.CSIDefault,
			invalidErr: v1alpha1.ErrInvalidCSI,
		},
		{
			typeName:   "CDI",
			newValue:   func() enumValue { return new(v1alpha1.CDI) },
			values:     []string{valueDefault, valueEnabled, valueDisabled},
			defaultsTo: v1alpha1.CDIDefault,
			invalidErr: v1alpha1.ErrInvalidCDI,
		},
		{
			typeName:   "MetricsServer",
			newValue:   func() enumValue { return new(v1alpha1.MetricsServer) },
			values:     []string{valueDefault, valueEnabled, valueDisabled},
			defaultsTo: v1alpha1.MetricsServerDefault,
			invalidErr: v1alpha1.ErrInvalidMetricsServer,
		},
		{
			typeName:   "LoadBalancer",
			newValue:   func() enumValue { return new(v1alpha1.LoadBalancer) },
			values:     []string{valueDefault, valueEnabled, valueDisabled},
			defaultsTo: v1alpha1.LoadBalancerDefault,
			invalidErr: v1alpha1.ErrInvalidLoadBalancer,
		},
	}
}

// policyEnumSpecs registers the security/policy toggle enums.
func policyEnumSpecs() []enumSpec {
	return []enumSpec{
		{
			typeName:   "CertManager",
			newValue:   func() enumValue { return new(v1alpha1.CertManager) },
			values:     []string{valueEnabled, valueDisabled},
			defaultsTo: v1alpha1.CertManagerDisabled,
			invalidErr: v1alpha1.ErrInvalidCertManager,
		},
		{
			typeName:   "ImageVerification",
			newValue:   func() enumValue { return new(v1alpha1.ImageVerification) },
			values:     []string{valueEnabled, valueDisabled},
			defaultsTo: v1alpha1.ImageVerificationDisabled,
			invalidErr: v1alpha1.ErrInvalidImageVerification,
		},
		{
			typeName:   "PolicyEngine",
			newValue:   func() enumValue { return new(v1alpha1.PolicyEngine) },
			values:     []string{valueNone, "Kyverno", "Gatekeeper"},
			defaultsTo: v1alpha1.PolicyEngineNone,
			invalidErr: v1alpha1.ErrInvalidPolicyEngine,
		},
		{
			typeName:   "IngressFirewall",
			newValue:   func() enumValue { return new(v1alpha1.IngressFirewall) },
			values:     []string{valueEnabled, valueDisabled},
			defaultsTo: v1alpha1.IngressFirewallEnabled,
			invalidErr: v1alpha1.ErrInvalidIngressFirewall,
		},
	}
}

// autoscalerEnumSpecs registers the autoscaler-related enums.
func autoscalerEnumSpecs() []enumSpec {
	return []enumSpec{
		{
			typeName:   "NodeAutoscaling",
			newValue:   func() enumValue { return new(v1alpha1.NodeAutoscaling) },
			values:     []string{valueEnabled, valueDisabled},
			defaultsTo: v1alpha1.NodeAutoscalingDisabled,
			invalidErr: v1alpha1.ErrInvalidNodeAutoscaling,
		},
		{
			typeName:   "AutoscalerExpander",
			newValue:   func() enumValue { return new(v1alpha1.AutoscalerExpander) },
			values:     []string{"Price", "LeastWaste", "LeastNodes", "Random"},
			defaultsTo: v1alpha1.AutoscalerExpanderLeastWaste,
			invalidErr: v1alpha1.ErrInvalidAutoscalerExpander,
		},
		{
			typeName:   "PodAutoscalerHorizontal",
			newValue:   func() enumValue { return new(v1alpha1.PodAutoscalerHorizontal) },
			values:     []string{valueEnabled, valueDisabled},
			defaultsTo: v1alpha1.PodAutoscalerHorizontalDisabled,
			invalidErr: v1alpha1.ErrInvalidPodAutoscalerHorizontal,
		},
		{
			typeName:   "PodAutoscalerVertical",
			newValue:   func() enumValue { return new(v1alpha1.PodAutoscalerVertical) },
			values:     []string{valueEnabled, valueDisabled},
			defaultsTo: v1alpha1.PodAutoscalerVerticalDisabled,
			invalidErr: v1alpha1.ErrInvalidPodAutoscalerVertical,
		},
	}
}

// TestEnumConformance runs the shared conformance checks for every registered
// enum type.
func TestEnumConformance(t *testing.T) {
	t.Parallel()

	for _, spec := range enumSpecs() {
		t.Run(spec.typeName, func(t *testing.T) {
			t.Parallel()

			assertEnumIdentity(t, spec)
			assertEnumSetRoundTrips(t, spec)
			assertEnumSetRejectsInvalid(t, spec)
		})
	}
}

// assertEnumIdentity checks ValidValues(), Type(), String() and Default().
func assertEnumIdentity(t *testing.T, spec enumSpec) {
	t.Helper()

	value := spec.newValue()
	assert.Equal(t, spec.values, value.ValidValues(), "ValidValues() mismatch")
	assert.Equal(t, spec.typeName, value.Type(), "Type() mismatch")
	assert.Empty(t, value.String(), "zero value String() should be empty")

	assert.Equal(t, spec.defaultsTo, value.Default(), "Default() mismatch")
	assert.Contains(
		t, spec.values, fmt.Sprintf("%v", value.Default()),
		"Default() must be one of ValidValues()",
	)
}

// assertEnumSetRoundTrips checks that Set() accepts every valid value in its
// canonical, lower-case, and upper-case spellings, storing the canonical one.
func assertEnumSetRoundTrips(t *testing.T, spec enumSpec) {
	t.Helper()

	for _, valid := range spec.values {
		for _, input := range []string{valid, strings.ToLower(valid), strings.ToUpper(valid)} {
			value := spec.newValue()
			require.NoError(t, value.Set(input), "Set(%q) should succeed", input)
			assert.Equal(t, valid, value.String(), "Set(%q) should store canonical spelling", input)
		}
	}
}

// assertEnumSetRejectsInvalid checks that Set() rejects unknown and empty
// inputs with the registered sentinel error, and that the error message names
// the rejected input and lists every valid option.
func assertEnumSetRejectsInvalid(t *testing.T, spec enumSpec) {
	t.Helper()

	// Use a distinctive token that does not appear in any sentinel ("invalid …")
	// or valid option, so the echo assertion below actually verifies the rejected
	// input is named — asserting "invalid" would pass via the sentinel text alone.
	const rejected = "bogus-not-a-value"

	value := spec.newValue()
	err := value.Set(rejected)
	require.ErrorIs(t, err, spec.invalidErr, "Set(%q) should wrap the sentinel", rejected)
	require.ErrorContains(t, err, rejected, "error should name the rejected input")

	for _, valid := range spec.values {
		require.ErrorContains(t, err, valid, "error should list valid option %q", valid)
	}

	empty := spec.newValue()
	require.ErrorIs(t, empty.Set(""), spec.invalidErr, "Set(\"\") should wrap the sentinel")
}

// ---------------------------------------------------------------------------
// ValidValues-only enums (no Set(); EnumValuer for the schema generator).
// ---------------------------------------------------------------------------

func TestTaintEffect_ValidValues(t *testing.T) {
	t.Parallel()

	var effect v1alpha1.TaintEffect

	assert.Equal(t, []string{"NoSchedule", "PreferNoSchedule", "NoExecute"}, effect.ValidValues())
	assert.Equal(
		t,
		[]v1alpha1.TaintEffect{
			v1alpha1.TaintEffectNoSchedule,
			v1alpha1.TaintEffectPreferNoSchedule,
			v1alpha1.TaintEffectNoExecute,
		},
		v1alpha1.ValidTaintEffects(),
	)
}

// ---------------------------------------------------------------------------
// Distribution semantics beyond the shared enum contract.
// ---------------------------------------------------------------------------

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

// distributionNamingCase captures the full naming surface of one distribution:
// default cluster name, default config file, context naming convention, and
// the derived default context name.
type distributionNamingCase struct {
	name            string
	distribution    v1alpha1.Distribution
	defaultName     string
	configName      string
	contextForFoo   string
	expectedContext string
}

// Expected default config names, pinned as literals so generator/constant
// changes surface here.
const (
	expectedKindConfig  = "kind.yaml"
	expectedTalosConfig = "talos"
)

// distributionNamingCases covers all six distributions plus the
// unknown-distribution fallbacks.
func distributionNamingCases() []distributionNamingCase {
	return []distributionNamingCase{
		{
			name:            "vanilla",
			distribution:    v1alpha1.DistributionVanilla,
			defaultName:     "kind",
			configName:      expectedKindConfig,
			contextForFoo:   "kind-foo",
			expectedContext: "kind-kind",
		},
		{
			name:            "k3s",
			distribution:    v1alpha1.DistributionK3s,
			defaultName:     "k3d-default",
			configName:      "k3d.yaml",
			contextForFoo:   "k3d-foo",
			expectedContext: "k3d-k3d-default",
		},
		{
			name:            "talos",
			distribution:    v1alpha1.DistributionTalos,
			defaultName:     "talos-default",
			configName:      expectedTalosConfig,
			contextForFoo:   "admin@foo",
			expectedContext: "admin@talos-default",
		},
		{
			name:            "vcluster",
			distribution:    v1alpha1.DistributionVCluster,
			defaultName:     "vcluster-default",
			configName:      "vcluster.yaml",
			contextForFoo:   "vcluster-docker_foo",
			expectedContext: "vcluster-docker_vcluster-default",
		},
		{
			name:            "kwok",
			distribution:    v1alpha1.DistributionKWOK,
			defaultName:     "kwok-default",
			configName:      "kwok",
			contextForFoo:   "kwok-foo",
			expectedContext: "kwok-kwok-default",
		},
		{
			name:            "eks",
			distribution:    v1alpha1.DistributionEKS,
			defaultName:     "eks-default",
			configName:      "eks.yaml",
			contextForFoo:   "foo.eksctl.io",
			expectedContext: "eks-default.eksctl.io",
		},
		{
			name:            "unknown_falls_back",
			distribution:    v1alpha1.Distribution("Unknown"),
			defaultName:     "kind",
			configName:      expectedKindConfig,
			contextForFoo:   "",
			expectedContext: "",
		},
	}
}

func TestDistribution_NamingConventions(t *testing.T) {
	t.Parallel()

	for _, testCase := range distributionNamingCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.defaultName, testCase.distribution.DefaultClusterName())
			assert.Equal(
				t,
				testCase.configName,
				v1alpha1.ExpectedDistributionConfigName(testCase.distribution),
			)
			assert.Equal(t, testCase.contextForFoo, testCase.distribution.ContextName("foo"))
			assert.Equal(
				t,
				testCase.expectedContext,
				v1alpha1.ExpectedContextName(testCase.distribution),
			)
		})
	}
}

func TestDistribution_ContextName_EmptyClusterName(t *testing.T) {
	t.Parallel()

	dist := v1alpha1.DistributionVanilla
	assert.Empty(t, dist.ContextName(""))
}

func TestDistribution_ProvidesCDIByDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		expected     bool
	}{
		{"talos_provides_cdi", v1alpha1.DistributionTalos, true},
		{"vanilla_no_cdi", v1alpha1.DistributionVanilla, false},
		{"k3s_no_cdi", v1alpha1.DistributionK3s, false},
		{"vcluster_no_cdi", v1alpha1.DistributionVCluster, false},
		{"unknown_no_cdi", v1alpha1.Distribution("unknown"), false},
		{"empty_no_cdi", v1alpha1.Distribution(""), false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := testCase.distribution.ProvidesCDIByDefault()
			assert.Equal(t, testCase.expected, result)
		})
	}
}

// providesByDefaultCase is a shared table row for the per-provider
// "provides X by default" distribution tests (CSI, LoadBalancer).
type providesByDefaultCase struct {
	name         string
	distribution v1alpha1.Distribution
	provider     v1alpha1.Provider
	expected     bool
}

// runProvidesByDefault runs fn for each case and asserts the expected result.
// It is shared by the CSI and LoadBalancer by-default tests, whose tables differ
// but whose execution is identical.
func runProvidesByDefault(
	t *testing.T,
	cases []providesByDefaultCase,
	provides func(v1alpha1.Distribution, v1alpha1.Provider) bool,
) {
	t.Helper()

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.expected, provides(testCase.distribution, testCase.provider))
		})
	}
}

func TestDistribution_ProvidesCSIByDefault(t *testing.T) {
	t.Parallel()

	runProvidesByDefault(t, []providesByDefaultCase{
		{"k3s_docker_provides_csi", v1alpha1.DistributionK3s, v1alpha1.ProviderDocker, true},
		{"vanilla_docker_no_csi", v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker, false},
		{"talos_docker_no_csi", v1alpha1.DistributionTalos, v1alpha1.ProviderDocker, false},
		{"talos_hetzner_provides_csi", v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner, true},
		{"vcluster_docker_no_csi", v1alpha1.DistributionVCluster, v1alpha1.ProviderDocker, false},
		{"eks_aws_provides_csi", v1alpha1.DistributionEKS, v1alpha1.ProviderAWS, true},
		{"unknown_no_csi", v1alpha1.Distribution("unknown"), v1alpha1.ProviderDocker, false},
	}, func(distribution v1alpha1.Distribution, provider v1alpha1.Provider) bool {
		return distribution.ProvidesCSIByDefault(provider)
	})
}

func TestDistribution_ProvidesLoadBalancerByDefault(t *testing.T) {
	t.Parallel()

	runProvidesByDefault(t, []providesByDefaultCase{
		{"k3s_docker_provides_lb", v1alpha1.DistributionK3s, v1alpha1.ProviderDocker, true},
		{"vanilla_docker_no_lb", v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker, false},
		{"talos_docker_no_lb", v1alpha1.DistributionTalos, v1alpha1.ProviderDocker, false},
		{"talos_hetzner_provides_lb", v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner, true},
		{
			"vcluster_docker_provides_lb",
			v1alpha1.DistributionVCluster,
			v1alpha1.ProviderDocker,
			true,
		},
		{"eks_aws_provides_lb", v1alpha1.DistributionEKS, v1alpha1.ProviderAWS, true},
		{"unknown_no_lb", v1alpha1.Distribution("Unknown"), v1alpha1.ProviderDocker, false},
	}, func(distribution v1alpha1.Distribution, provider v1alpha1.Provider) bool {
		return distribution.ProvidesLoadBalancerByDefault(provider)
	})
}

// ---------------------------------------------------------------------------
// Provider semantics beyond the shared enum contract.
// ---------------------------------------------------------------------------

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
		{"hetzner_for_vanilla", v1alpha1.ProviderHetzner, v1alpha1.DistributionVanilla},
		{"hetzner_for_k3s", v1alpha1.ProviderHetzner, v1alpha1.DistributionK3s},
		{"omni_for_talos", v1alpha1.ProviderOmni, v1alpha1.DistributionTalos},
		{"kubernetes_for_vanilla", v1alpha1.ProviderKubernetes, v1alpha1.DistributionVanilla},
		{"kubernetes_for_k3s", v1alpha1.ProviderKubernetes, v1alpha1.DistributionK3s},
		{"kubernetes_for_talos", v1alpha1.ProviderKubernetes, v1alpha1.DistributionTalos},
		{"kubernetes_for_vcluster", v1alpha1.ProviderKubernetes, v1alpha1.DistributionVCluster},
		{"kubernetes_for_kwok", v1alpha1.ProviderKubernetes, v1alpha1.DistributionKWOK},
		{"aws_for_eks", v1alpha1.ProviderAWS, v1alpha1.DistributionEKS},
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

// ---------------------------------------------------------------------------
// EffectiveValue semantics (Default → concrete resolution).
// ---------------------------------------------------------------------------

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

			result := testCase.ms.EffectiveValue(testCase.distribution, v1alpha1.ProviderDocker)
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
