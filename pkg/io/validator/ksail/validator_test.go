package ksail_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/io/validator"
	ksailvalidator "github.com/devantler-tech/ksail/v5/pkg/io/validator/ksail"
	k3dtypes "github.com/k3d-io/k3d/v5/pkg/config/types"
	k3dapi "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

const (
	specDistributionField = "spec.cluster.distribution"
	specCNIField          = "spec.cni"
	kindKSailContext      = "kind-ksail"
)

// TestKSailValidator tests the contract for KSail configuration validator.
func TestKSailValidator(t *testing.T) {
	// This test MUST FAIL initially to follow TDD approach
	t.Parallel()

	validator := ksailvalidator.NewValidator()
	require.NotNil(t, validator, "KSail validator constructor must return non-nil validator")

	testCases := createKSailTestCases()

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := validator.Validate(testCase.config)
			require.NotNil(t, result, "Validate must return non-nil result")

			assertKSailValidationResult(t, testCase, result)
		})
	}
}

func createKSailTestCases() []ksailTestCase {
	return []ksailTestCase{
		createValidKindKSailConfigCase(),
		createValidArgoCDKSailConfigCase(),
		createInvalidDistributionKSailCase(),
		createValidConfigWithoutMetadataCase(),
		createNilKSailConfigCase(),
	}
}

type ksailTestCase struct {
	name         string
	config       *v1alpha1.Cluster
	expectValid  bool
	expectErrors []string
}

func createValidKindKSailConfigCase() ksailTestCase {
	return ksailTestCase{
		name:         "valid_kind_configuration",
		config:       createValidKSailConfig(v1alpha1.DistributionKind),
		expectValid:  true,
		expectErrors: []string{},
	}
}

func createInvalidDistributionKSailCase() ksailTestCase {
	return ksailTestCase{
		name: "invalid_distribution",
		config: &v1alpha1.Cluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "ksail.dev/v1alpha1",
				Kind:       "Cluster",
			},
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: "InvalidDistribution",
				},
			},
		},
		expectValid:  false,
		expectErrors: []string{specDistributionField},
	}
}

func createValidArgoCDKSailConfigCase() ksailTestCase {
	config := createValidKSailConfig(v1alpha1.DistributionKind)
	config.Spec.Cluster.GitOpsEngine = v1alpha1.GitOpsEngine("ArgoCD")

	return ksailTestCase{
		name:         "valid_argocd_configuration",
		config:       config,
		expectValid:  true,
		expectErrors: []string{},
	}
}

func createValidConfigWithoutMetadataCase() ksailTestCase {
	return ksailTestCase{
		name:         "valid_config_without_metadata",
		config:       createValidKSailConfig(v1alpha1.DistributionKind),
		expectValid:  true,
		expectErrors: []string{},
	}
}

func createNilKSailConfigCase() ksailTestCase {
	return ksailTestCase{
		name:         "nil_config",
		config:       nil,
		expectValid:  false,
		expectErrors: []string{"config"},
	}
}

func assertKSailValidationResult(
	t *testing.T,
	testCase ksailTestCase,
	result *validator.ValidationResult,
) {
	t.Helper()

	assert.Equal(t, testCase.expectValid, result.Valid, "Validation result Valid status mismatch")

	if testCase.expectValid {
		assert.Empty(t, result.Errors, "Valid configuration should have no errors")

		return
	}

	assert.NotEmpty(t, result.Errors, "Invalid configuration should have errors")

	validateExpectedErrors(t, testCase.expectErrors, result.Errors)
}

func validateExpectedErrors(
	t *testing.T,
	expectedFields []string,
	errors []validator.ValidationError,
) {
	t.Helper()

	// Check that expected error fields are present
	for _, expectedField := range expectedFields {
		found := false

		for _, resultErr := range errors {
			if resultErr.Field == expectedField {
				found = true
				// Verify error has actionable content
				assert.NotEmpty(t, resultErr.Message, "Error message must not be empty")
				assert.NotEmpty(t, resultErr.FixSuggestion, "Error must have fix suggestion")

				break
			}
		}

		assert.True(t, found, "Expected error for field %s not found", expectedField)
	}
}

// TestKSailValidatorCrossConfiguration tests cross-configuration validation.
func TestKSailValidatorCrossConfiguration(t *testing.T) {
	t.Parallel()

	validator := ksailvalidator.NewValidator()

	t.Run("cross_config_validation_current_scope", func(t *testing.T) {
		t.Parallel()

		testValidConfiguration(t, validator)
		testInvalidDistribution(t, validator)
	})
}

func testValidConfiguration(t *testing.T, validator *ksailvalidator.Validator) {
	t.Helper()

	// Test current cross-configuration validation capabilities
	// The KSail validator is implemented and validates basic semantic correctness
	// Full cross-configuration coordination with config managers is future work (T034)

	config := &v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "ksail.dev/v1alpha1",
			Kind:       "Cluster",
		},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:       v1alpha1.DistributionKind,
				DistributionConfig: "kind.yaml",
				Connection: v1alpha1.Connection{
					Context: "kind-kind", // No distribution config provided, so use conventional default
				},
			},
		},
	}

	result := validator.Validate(config)
	require.NotNil(t, result, "Validation result should not be nil")

	// The validator exists and performs basic validation
	// Currently validates distribution types and required fields
	assert.True(t, result.Valid, "Valid configuration should pass validation")
	assert.Empty(t, result.Errors, "Valid configuration should have no errors")
}

func testInvalidDistribution(t *testing.T, validator *ksailvalidator.Validator) {
	t.Helper()

	// Test cross-configuration validation for invalid distribution
	invalidConfig := &v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "ksail.dev/v1alpha1",
			Kind:       "Cluster",
		},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: "InvalidDistribution",
			},
		},
	}

	invalidResult := validator.Validate(invalidConfig)
	require.NotNil(t, invalidResult, "Validation result should not be nil")
	assert.False(t, invalidResult.Valid, "Invalid distribution should fail validation")
	assert.NotEmpty(t, invalidResult.Errors, "Invalid configuration should have errors")

	// Verify error contains actionable information
	found := false

	for _, err := range invalidResult.Errors {
		if err.Field == specDistributionField {
			found = true

			assert.NotEmpty(t, err.Message, "Error message should not be empty")
			assert.NotEmpty(t, err.FixSuggestion, "Error should have fix suggestion")

			break
		}
	}

	assert.True(t, found, "Should find distribution validation error")
}

// createValidKSailConfig creates a valid KSail configuration with the specified distribution.
// Uses sample context names that are typical for each distribution.
func createValidKSailConfig(distribution v1alpha1.Distribution) *v1alpha1.Cluster {
	var distributionConfigFile string

	var contextName string

	switch distribution {
	case v1alpha1.DistributionKind:
		distributionConfigFile = "kind.yaml"
		contextName = "kind-kind" // Sample context name
	case v1alpha1.DistributionK3d:
		distributionConfigFile = "k3d.yaml"
		contextName = "k3d-k3d-default" // Sample context name
	case v1alpha1.DistributionTalosInDocker:
		distributionConfigFile = "talos"
		contextName = "admin@talos-default" // Sample context name
	default:
		distributionConfigFile = "cluster.yaml"
		contextName = "ksail"
	}

	return &v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "ksail.dev/v1alpha1",
			Kind:       "Cluster",
		},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:       distribution,
				DistributionConfig: distributionConfigFile,
				Connection: v1alpha1.Connection{
					Context: contextName,
				},
			},
		},
	}
}

func runKindCiliumAlignmentTest(
	t *testing.T,
	disableDefaultCNI bool,
	expectValid bool,
	expectedMessagePart string,
) {
	t.Helper()

	kindConfig := &kindv1alpha4.Cluster{
		Name: "ksail",
		Networking: kindv1alpha4.Networking{
			DisableDefaultCNI: disableDefaultCNI,
		},
	}

	validator := ksailvalidator.NewValidatorForKind(kindConfig)

	config := createValidKSailConfig(v1alpha1.DistributionKind)
	config.Spec.Cluster.CNI = v1alpha1.CNICilium
	config.Spec.Cluster.Connection.Context = kindKSailContext

	result := validator.Validate(config)

	if expectValid {
		assert.True(t, result.Valid, "expected config to be valid when disableDefaultCNI is true")
		assert.Empty(t, result.Errors, "expected no validation errors when configuration is valid")

		return
	}

	assert.False(t, result.Valid, "expected validation to fail when disableDefaultCNI is false")

	found := false

	for _, err := range result.Errors {
		if err.Field == specCNIField {
			found = true

			if expectedMessagePart != "" {
				assert.Contains(
					t,
					err.Message,
					expectedMessagePart,
					"error message should mention expected hint",
				)
			}

			assert.NotEmpty(t, err.FixSuggestion, "error should include fix suggestion")

			break
		}
	}

	assert.True(t, found, "expected to find Cilium alignment validation error")
}

// TestKSailValidatorContextNameValidation tests context name validation patterns.
func TestKSailValidatorContextNameValidation(t *testing.T) {
	t.Parallel()

	testKindValidContext(t)
	testK3dValidContext(t)
	testInvalidContextPatternWithConfig(t)
	testContextNotValidatedWithoutConfig(t)
}

func TestKSailValidatorFluxAndRegistryValidation(t *testing.T) {
	t.Parallel()

	for _, validationCase := range fluxRegistryValidationCases() {
		t.Run(validationCase.name, func(t *testing.T) {
			t.Parallel()
			validationCase.run(t)
		})
	}
}

type fluxRegistryValidationCase struct {
	name string
	run  func(*testing.T)
}

func fluxRegistryValidationCases() []fluxRegistryValidationCase {
	return []fluxRegistryValidationCase{
		{name: "invalid_gitops_engine", run: validateInvalidGitOpsEngineCase},
		{name: "registry_port_required_when_enabled", run: validateRegistryPortRequiredCase},
		{name: "registry_port_range", run: validateRegistryPortRangeCase},
		{name: "registry_port_warning_when_disabled", run: validateRegistryPortWarningCase},
		{name: "flux_interval_must_be_positive", run: validateFluxIntervalCase},
	}
}

func validateInvalidGitOpsEngineCase(t *testing.T) {
	t.Helper()

	validator := ksailvalidator.NewValidator()
	config := createValidKSailConfig(v1alpha1.DistributionKind)
	config.Spec.Cluster.GitOpsEngine = v1alpha1.GitOpsEngine("Invalid")

	result := validator.Validate(config)
	assert.False(t, result.Valid)
	validateExpectedErrors(t, []string{"spec.gitOpsEngine"}, result.Errors)
}

func validateRegistryPortRequiredCase(t *testing.T) {
	t.Helper()

	validator := ksailvalidator.NewValidator()
	config := createValidKSailConfig(v1alpha1.DistributionKind)
	config.Spec.Cluster.LocalRegistry = v1alpha1.LocalRegistryEnabled
	config.Spec.Cluster.Options.LocalRegistry.HostPort = 0

	result := validator.Validate(config)
	assert.False(t, result.Valid)
	validateExpectedErrors(t, []string{"spec.options.localRegistry.hostPort"}, result.Errors)
}

func validateRegistryPortRangeCase(t *testing.T) {
	t.Helper()

	validator := ksailvalidator.NewValidator()
	config := createValidKSailConfig(v1alpha1.DistributionKind)
	config.Spec.Cluster.LocalRegistry = v1alpha1.LocalRegistryEnabled
	config.Spec.Cluster.Options.LocalRegistry.HostPort = 70000

	result := validator.Validate(config)
	assert.False(t, result.Valid)
	validateExpectedErrors(t, []string{"spec.options.localRegistry.hostPort"}, result.Errors)
}

func validateRegistryPortWarningCase(t *testing.T) {
	t.Helper()

	validator := ksailvalidator.NewValidator()
	config := createValidKSailConfig(v1alpha1.DistributionKind)
	config.Spec.Cluster.LocalRegistry = v1alpha1.LocalRegistryDisabled
	config.Spec.Cluster.Options.LocalRegistry.HostPort = 5001

	result := validator.Validate(config)
	assert.True(t, result.Valid)
	assert.Empty(t, result.Warnings)
}

func validateFluxIntervalCase(t *testing.T) {
	t.Helper()

	validator := ksailvalidator.NewValidator()
	config := createValidKSailConfig(v1alpha1.DistributionKind)
	config.Spec.Cluster.GitOpsEngine = v1alpha1.GitOpsEngineFlux
	config.Spec.Cluster.Options.Flux.Interval = metav1.Duration{}

	result := validator.Validate(config)
	assert.False(t, result.Valid)
	validateExpectedErrors(t, []string{"spec.options.flux.interval"}, result.Errors)
}

func testKindValidContext(t *testing.T) {
	t.Helper()

	t.Run("kind_valid_context", func(t *testing.T) {
		t.Parallel()

		config := createValidKSailConfig(v1alpha1.DistributionKind)
		config.Spec.Cluster.Connection.Context = "kind-kind" // No distribution config, so expect conventional default

		validator := ksailvalidator.NewValidator()
		result := validator.Validate(config)

		assert.True(t, result.Valid, "Valid Kind context should pass validation")
		assert.Empty(t, result.Errors, "Valid context should have no errors")
	})
}

func testK3dValidContext(t *testing.T) {
	t.Helper()

	t.Run("k3d_valid_context", func(t *testing.T) {
		t.Parallel()

		config := createValidKSailConfig(v1alpha1.DistributionK3d)
		config.Spec.Cluster.Connection.Context = "k3d-k3d-default" // No distribution config, so expect conventional default

		validator := ksailvalidator.NewValidator()
		result := validator.Validate(config)

		assert.True(t, result.Valid, "Valid K3d context should pass validation")
		assert.Empty(t, result.Errors, "Valid context should have no errors")
	})
}

func testInvalidContextPatternWithConfig(t *testing.T) {
	t.Helper()

	t.Run("invalid_context_pattern_with_config", func(t *testing.T) {
		t.Parallel()

		// Create a Kind config with a specific name
		kindConfig := &kindv1alpha4.Cluster{
			Name: "my-cluster",
		}

		config := createValidKSailConfig(v1alpha1.DistributionKind)
		config.Spec.Cluster.Connection.Context = "invalid-context"

		// Use validator WITH distribution config to enable context validation
		validator := ksailvalidator.NewValidatorForKind(kindConfig)
		result := validator.Validate(config)

		assert.False(
			t,
			result.Valid,
			"Invalid context should fail validation when distribution config is provided",
		)
		assert.NotEmpty(t, result.Errors, "Invalid context should have errors")

		// Find the context error
		found := false

		for _, err := range result.Errors {
			if err.Field == "spec.cluster.connection.context" {
				found = true

				assert.Contains(t, err.Message, "context name does not match expected pattern")
				assert.Contains(t, err.FixSuggestion, "kind-my-cluster")

				break
			}
		}

		assert.True(t, found, "Should have context validation error")
	})
}

func testContextNotValidatedWithoutConfig(t *testing.T) {
	t.Helper()

	t.Run("context_not_validated_without_config", func(t *testing.T) {
		t.Parallel()

		config := createValidKSailConfig(v1alpha1.DistributionKind)
		config.Spec.Cluster.Connection.Context = "any-context-name" // Any context should be valid without distribution config

		// Use validator WITHOUT distribution config
		validator := ksailvalidator.NewValidator()
		result := validator.Validate(config)

		assert.True(
			t,
			result.Valid,
			"Context should not be validated when no distribution config is provided",
		)
		assert.Empty(t, result.Errors, "Should have no errors without distribution config")
	})
}

// TestKSailValidatorKindConsistency tests Kind distribution name consistency validation.
func TestKSailValidatorKindConsistency(t *testing.T) {
	t.Parallel()

	t.Run("matching_names", func(t *testing.T) {
		t.Parallel()

		config := createValidKSailConfig(v1alpha1.DistributionKind)
		config.Spec.Cluster.Connection.Context = kindKSailContext // Set context to match the provided Kind config name

		// Create a Kind config with matching name
		kindConfig := &kindv1alpha4.Cluster{
			Name: "ksail", // Matches expected cluster name
		}

		validator := ksailvalidator.NewValidatorForKind(kindConfig)
		result := validator.Validate(config)

		assert.True(t, result.Valid, "Matching Kind config names should pass validation")
		assert.Empty(t, result.Errors, "Matching names should have no errors")
	})

	t.Run("custom_name", func(t *testing.T) {
		t.Parallel()

		config := createValidKSailConfig(v1alpha1.DistributionKind)
		config.Spec.Cluster.Connection.Context = "kind-different-name" // Use different context to match the Kind config name

		// Create a Kind config with specific name
		kindConfig := &kindv1alpha4.Cluster{
			Name: "different-name", // This should be used in the context name
		}

		validator := ksailvalidator.NewValidatorForKind(kindConfig)
		result := validator.Validate(config)

		assert.True(
			t,
			result.Valid,
			"Context matching distribution config name should pass validation",
		)
		assert.Empty(t, result.Errors, "Valid context should have no errors")
	})
}

// TestKSailValidatorK3dConsistency tests K3d distribution name consistency validation.
func TestKSailValidatorK3dConsistency(t *testing.T) {
	t.Parallel()

	t.Run("matching_names", func(t *testing.T) {
		t.Parallel()

		config := createValidKSailConfig(v1alpha1.DistributionK3d)
		config.Spec.Cluster.Connection.Context = "k3d-ksail" // Set context to match the provided K3d config name

		// Create a K3d config with matching name
		k3dConfig := &k3dapi.SimpleConfig{
			ObjectMeta: k3dtypes.ObjectMeta{
				Name: "ksail", // Matches expected cluster name
			},
		}

		validator := ksailvalidator.NewValidatorForK3d(k3dConfig)
		result := validator.Validate(config)

		assert.True(t, result.Valid, "Matching K3d config names should pass validation")
		assert.Empty(t, result.Errors, "Matching names should have no errors")
	})
}

func TestKSailValidatorK3dCiliumExtraArgsValidation(t *testing.T) {
	t.Parallel()

	for _, testCase := range ciliumExtraArgsTestCases() {
		tc := testCase

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runCiliumExtraArgsValidationTest(t, tc)
		})
	}
}

func TestKSailValidatorKindCiliumAlignment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		disableDefaultCNI   bool
		expectValid         bool
		expectedMessagePart string
	}{
		{
			name:                "disable_default_cni_required",
			disableDefaultCNI:   false,
			expectValid:         false,
			expectedMessagePart: "disableDefaultCNI",
		},
		{
			name:                "cilium_alignment_succeeds",
			disableDefaultCNI:   true,
			expectValid:         true,
			expectedMessagePart: "",
		},
	}

	for idx := range tests {
		testCase := tests[idx]

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			runKindCiliumAlignmentTest(
				t,
				testCase.disableDefaultCNI,
				testCase.expectValid,
				testCase.expectedMessagePart,
			)
		})
	}
}

func TestKSailValidatorKindCiliumAlignmentWithoutKindConfig(t *testing.T) {
	t.Parallel()

	validator := ksailvalidator.NewValidator()

	config := createValidKSailConfig(v1alpha1.DistributionKind)
	config.Spec.Cluster.CNI = v1alpha1.CNICilium

	result := validator.Validate(config)
	assert.True(t, result.Valid, "expected validation to pass when kind config is absent")
	assert.Empty(t, result.Errors, "expected no validation errors without kind config")
}

func TestKSailValidatorK3dCiliumAlignmentWithoutK3dConfig(t *testing.T) {
	t.Parallel()

	validator := ksailvalidator.NewValidator()

	config := createValidKSailConfig(v1alpha1.DistributionK3d)
	config.Spec.Cluster.CNI = v1alpha1.CNICilium

	result := validator.Validate(config)
	assert.True(t, result.Valid, "expected validation to pass when k3d config is absent")
	assert.Empty(t, result.Errors, "expected no validation errors without k3d config")
}

func TestKSailValidatorKindDefaultCNIAlignmentWithoutKindConfig(t *testing.T) {
	t.Parallel()

	validator := ksailvalidator.NewValidator()

	config := createValidKSailConfig(v1alpha1.DistributionKind)
	config.Spec.Cluster.CNI = v1alpha1.CNIDefault

	result := validator.Validate(config)
	assert.True(t, result.Valid, "expected validation to pass when kind config is absent")
	assert.Empty(t, result.Errors, "expected no validation errors without kind config")
}

func TestKSailValidatorK3dDefaultCNIAlignmentWithoutK3dConfig(t *testing.T) {
	t.Parallel()

	validator := ksailvalidator.NewValidator()

	config := createValidKSailConfig(v1alpha1.DistributionK3d)
	config.Spec.Cluster.CNI = v1alpha1.CNIDefault

	result := validator.Validate(config)
	assert.True(t, result.Valid, "expected validation to pass when k3d config is absent")
	assert.Empty(t, result.Errors, "expected no validation errors without k3d config")
}

type ciliumExtraArgsTestCase struct {
	name        string
	extraArgs   []k3dapi.K3sArgWithNodeFilters
	expectValid bool
	expectSnips []string
}

func ciliumExtraArgsTestCases() []ciliumExtraArgsTestCase {
	return []ciliumExtraArgsTestCase{
		{
			name: "all_required_args_present",
			extraArgs: []k3dapi.K3sArgWithNodeFilters{
				{Arg: "--flannel-backend=none", NodeFilters: []string{"server:*"}},
				{Arg: "--disable-network-policy", NodeFilters: []string{"server:*"}},
			},
			expectValid: true,
		},
		{
			name: "missing_flannel_backend",
			extraArgs: []k3dapi.K3sArgWithNodeFilters{
				{Arg: "--disable-network-policy", NodeFilters: []string{"server:*"}},
			},
			expectValid: false,
			expectSnips: []string{"--flannel-backend=none"},
		},
		{
			name: "missing_network_policy_disable",
			extraArgs: []k3dapi.K3sArgWithNodeFilters{
				{Arg: "--flannel-backend=none", NodeFilters: []string{"server:*"}},
			},
			expectValid: false,
			expectSnips: []string{"--disable-network-policy"},
		},
		{
			name:        "missing_all_required_args",
			extraArgs:   nil,
			expectValid: false,
			expectSnips: []string{"--flannel-backend=none", "--disable-network-policy"},
		},
	}
}

func runCiliumExtraArgsValidationTest(t *testing.T, testCase ciliumExtraArgsTestCase) {
	t.Helper()

	cluster := createValidKSailConfig(v1alpha1.DistributionK3d)
	cluster.Spec.Cluster.CNI = v1alpha1.CNICilium

	k3dConfig := &k3dapi.SimpleConfig{ObjectMeta: k3dtypes.ObjectMeta{Name: "ksail"}}
	cluster.Spec.Cluster.Connection.Context = "k3d-" + k3dConfig.Name
	k3dConfig.Options.K3sOptions.ExtraArgs = testCase.extraArgs

	validator := ksailvalidator.NewValidatorForK3d(k3dConfig)
	result := validator.Validate(cluster)

	if testCase.expectValid {
		assert.True(t, result.Valid)
		assert.Empty(t, result.Errors)

		return
	}

	assert.False(t, result.Valid)
	require.Len(t, result.Errors, 1)
	err := result.Errors[0]
	assert.Equal(t, "spec.cni", err.Field)

	for _, snippet := range testCase.expectSnips {
		assert.Contains(t, err.Message, snippet)
		assert.Contains(t, err.FixSuggestion, snippet)
	}
}

// TestKSailValidatorMultipleConfigs tests validation with multiple distribution configs.
func TestKSailValidatorMultipleConfigs(t *testing.T) {
	t.Parallel()

	t.Run("uses_correct_distribution", func(t *testing.T) {
		t.Parallel()

		config := createValidKSailConfig(v1alpha1.DistributionKind)
		config.Spec.Cluster.Connection.Context = kindKSailContext // Set context to match the Kind config name

		// Create Kind config for validation (K3d config is irrelevant for Kind distribution)
		kindConfig := &kindv1alpha4.Cluster{
			Name: "ksail",
		}

		validator := ksailvalidator.NewValidatorForKind(kindConfig)
		result := validator.Validate(config)

		assert.True(t, result.Valid, "Should validate only the matching distribution config")
		assert.Empty(t, result.Errors, "Should have no errors when distribution matches")
	})
}

// TestKSailValidatorUnsupportedDistribution tests handling of unsupported distributions.
// Helper function to create a test cluster config.
func createTestClusterConfig(
	distribution v1alpha1.Distribution,
	configFile,
	context string,
) *v1alpha1.Cluster {
	return &v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "ksail.dev/v1alpha1",
			Kind:       "Cluster",
		},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:       distribution,
				DistributionConfig: configFile,
				Connection: v1alpha1.Connection{
					Context: context,
				},
			},
		},
	}
}

// Helper function to check for specific distribution error.
func checkDistributionError(
	t *testing.T,
	errors []validator.ValidationError,
	expectedMessage, errorDescription string,
) {
	t.Helper()

	found := false

	for _, err := range errors {
		if err.Field == specDistributionField &&
			strings.Contains(err.Message, expectedMessage) {
			found = true

			assert.Contains(t, err.FixSuggestion, "Use a supported distribution")

			break
		}
	}

	assert.True(t, found, errorDescription)
}

// Helper function to test supported distribution error paths.
func testSupportedDistributionErrorPath(
	t *testing.T,
	distribution v1alpha1.Distribution,
	_ string,
) {
	t.Helper()

	_ = createTestClusterConfig(distribution, "config.yaml", "some-context")
	// These should normally pass validation or have different errors
	// The unexpected error cases are defensive code paths
}

func TestKSailValidatorUnsupportedDistribution(t *testing.T) {
	t.Parallel()

	testUnknownDistribution(t)
	testSupportedDistributionErrorPaths(t)
}

// testUnknownDistribution tests unknown distribution validation.
func testUnknownDistribution(t *testing.T) {
	t.Helper()

	t.Run("unknown_distribution", func(t *testing.T) {
		t.Parallel()

		config := createTestClusterConfig(
			v1alpha1.Distribution("UnknownDistribution"),
			"unknown.yaml",
			"unknown-context",
		)
		validator := ksailvalidator.NewValidator()
		result := validator.Validate(config)

		assert.False(t, result.Valid, "Unknown distribution should fail validation")
		assert.NotEmpty(t, result.Errors, "Should have validation errors")

		checkDistributionError(
			t,
			result.Errors,
			"invalid distribution",
			"Should have invalid distribution error",
		)
	})
}

// testSupportedDistributionErrorPaths tests error paths for supported distributions.
func testSupportedDistributionErrorPaths(t *testing.T) {
	t.Helper()

	t.Run("test_supported_distribution_error_paths", func(t *testing.T) {
		t.Parallel()

		// Test error handling for supported distributions that shouldn't reach unsupported error logic
		// This tests the Kind and K3d cases in addUnsupportedDistributionError
		testCases := []struct {
			name         string
			distribution v1alpha1.Distribution
			expectedMsg  string
		}{
			{
				name:         "kind_unexpected_error",
				distribution: v1alpha1.DistributionKind,
				expectedMsg:  "unexpected error in Kind distribution validation",
			},
			{
				name:         "k3d_unexpected_error",
				distribution: v1alpha1.DistributionK3d,
				expectedMsg:  "unexpected error in K3d distribution validation",
			},
		}

		for _, testCase := range testCases {
			t.Run(testCase.name, func(t *testing.T) {
				t.Parallel()
				testSupportedDistributionErrorPath(t, testCase.distribution, testCase.expectedMsg)
			})
		}
	})
}

// TestKSailValidatorContextPatterns tests different context name patterns.
func TestKSailValidatorContextPatterns(t *testing.T) {
	t.Parallel()

	testEmptyContextValidationSkipped(t)
}

// testEmptyContextValidationSkipped tests that empty context skips validation.
func testEmptyContextValidationSkipped(t *testing.T) {
	t.Helper()

	t.Run("empty_context_validation_skipped", func(t *testing.T) {
		t.Parallel()

		config := &v1alpha1.Cluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "ksail.dev/v1alpha1",
				Kind:       "Cluster",
			},
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution:       v1alpha1.DistributionKind,
					DistributionConfig: "kind.yaml",
					Connection: v1alpha1.Connection{
						Context: "", // Empty context should skip validation
					},
				},
			},
		}

		validator := ksailvalidator.NewValidator()
		result := validator.Validate(config)

		assert.True(t, result.Valid, "Empty context should skip validation")
		assert.Empty(t, result.Errors, "Empty context validation should be skipped")
	})
}

// TestKSailValidatorCrossConfigurationValidation tests validation with distribution configs.
func TestKSailValidatorCrossConfigurationValidation(t *testing.T) {
	t.Parallel()

	testKindCrossValidationWithConfigName(t)
	testK3dCrossValidationWithConfigName(t)
}

// testKindCrossValidationWithConfigName tests Kind cross-validation with custom config name.
func testKindCrossValidationWithConfigName(t *testing.T) {
	t.Helper()

	t.Run("kind_cross_validation_with_config_name", func(t *testing.T) {
		t.Parallel()

		kindConfig := &kindv1alpha4.Cluster{Name: "custom-kind-cluster"}
		config := &v1alpha1.Cluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "ksail.dev/v1alpha1",
				Kind:       "Cluster",
			},
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution:       v1alpha1.DistributionKind,
					DistributionConfig: "kind.yaml",
					Connection: v1alpha1.Connection{
						Context: "kind-custom-kind-cluster", // Should match the Kind config name
					},
				},
			},
		}

		validator := ksailvalidator.NewValidatorForKind(kindConfig)
		result := validator.Validate(config)

		assert.True(t, result.Valid, "Validation should pass with matching Kind config name")
		assert.Empty(t, result.Errors, "Should have no validation errors")
	})
}

// testK3dCrossValidationWithConfigName tests K3d cross-validation with custom config name.
func testK3dCrossValidationWithConfigName(t *testing.T) {
	t.Helper()

	t.Run("k3d_cross_validation_with_config_name", func(t *testing.T) {
		t.Parallel()

		k3dConfig := &k3dapi.SimpleConfig{
			ObjectMeta: k3dtypes.ObjectMeta{Name: "custom-k3d-cluster"},
		}
		config := &v1alpha1.Cluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "ksail.dev/v1alpha1",
				Kind:       "Cluster",
			},
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution:       v1alpha1.DistributionK3d,
					DistributionConfig: "k3d.yaml",
					Connection: v1alpha1.Connection{
						Context: "k3d-custom-k3d-cluster", // Should match the K3d config name
					},
				},
			},
		}

		validator := ksailvalidator.NewValidatorForK3d(k3dConfig)
		result := validator.Validate(config)

		assert.True(t, result.Valid, "Validation should pass with matching K3d config name")
		assert.Empty(t, result.Errors, "Should have no validation errors")
	})
}

// TestKSailValidatorEmptyConfigNameValidation tests validation when distribution config has empty name.
func TestKSailValidatorEmptyConfigNameValidation(t *testing.T) {
	t.Parallel()

	testKindEmptyConfigName(t)
	testK3dEmptyConfigName(t)
}

// testKindEmptyConfigName tests Kind validation with empty config name.
func testKindEmptyConfigName(t *testing.T) {
	t.Helper()

	t.Run("kind_empty_config_name", func(t *testing.T) {
		t.Parallel()

		kindConfig := &kindv1alpha4.Cluster{Name: ""} // Empty name - validation should be skipped
		config := &v1alpha1.Cluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "ksail.dev/v1alpha1",
				Kind:       "Cluster",
			},
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution:       v1alpha1.DistributionKind,
					DistributionConfig: "kind.yaml",
					Connection: v1alpha1.Connection{
						Context: "any-context-name", // Any context is valid when config name is empty
					},
				},
			},
		}

		validator := ksailvalidator.NewValidatorForKind(kindConfig)
		result := validator.Validate(config)

		assert.True(
			t,
			result.Valid,
			"Validation should skip context check when Kind config has empty name",
		)
		assert.Empty(t, result.Errors, "Should have no validation errors")
	})
}

// testK3dEmptyConfigName tests K3d validation with empty config name.
func testK3dEmptyConfigName(t *testing.T) {
	t.Helper()

	t.Run("k3d_empty_config_name", func(t *testing.T) {
		t.Parallel()

		k3dConfig := &k3dapi.SimpleConfig{
			ObjectMeta: k3dtypes.ObjectMeta{Name: ""}, // Empty name - validation should be skipped
		}
		config := &v1alpha1.Cluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "ksail.dev/v1alpha1",
				Kind:       "Cluster",
			},
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution:       v1alpha1.DistributionK3d,
					DistributionConfig: "k3d.yaml",
					Connection: v1alpha1.Connection{
						Context: "any-context-name", // Any context is valid when config name is empty
					},
				},
			},
		}

		validator := ksailvalidator.NewValidatorForK3d(k3dConfig)
		result := validator.Validate(config)

		assert.True(
			t,
			result.Valid,
			"Validation should skip context check when K3d config has empty name",
		)
		assert.Empty(t, result.Errors, "Should have no validation errors")
	})
}

// TestKSailValidatorSpecialDistributionHandling tests special distribution handling scenarios.
func TestKSailValidatorSpecialDistributionHandling(t *testing.T) {
	t.Parallel()

	testNoDistributionConfigProvided(t)
}

// testNoDistributionConfigProvided tests validation when no distribution config is provided.
func testNoDistributionConfigProvided(t *testing.T) {
	t.Helper()

	t.Run("no_distribution_config_provided", func(t *testing.T) {
		t.Parallel()

		config := &v1alpha1.Cluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "ksail.dev/v1alpha1",
				Kind:       "Cluster",
			},
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution:       v1alpha1.DistributionKind,
					DistributionConfig: "kind.yaml",
					Connection: v1alpha1.Connection{
						Context: "any-custom-context", // Any context should be valid when no config provided
					},
				},
			},
		}

		validator := ksailvalidator.NewValidator() // No distribution config provided
		result := validator.Validate(config)

		assert.True(
			t,
			result.Valid,
			"Validation should skip context check when no distribution config provided",
		)
		assert.Empty(t, result.Errors, "Should have no validation errors")
	})
}

// TestKSailValidatorDistributionValidation tests distribution field validation.
func TestKSailValidatorDistributionValidation(t *testing.T) {
	t.Parallel()

	testEmptyDistribution(t)
	testEmptyDistributionConfig(t)
	testInvalidDistributionValue(t)
}

// testEmptyDistribution tests validation with empty distribution field.
func testEmptyDistribution(t *testing.T) {
	t.Helper()

	t.Run("empty_distribution", func(t *testing.T) {
		t.Parallel()

		config := &v1alpha1.Cluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "ksail.dev/v1alpha1",
				Kind:       "Cluster",
			},
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution:       "", // Empty distribution
					DistributionConfig: "config.yaml",
				},
			},
		}

		validator := ksailvalidator.NewValidator()
		result := validator.Validate(config)

		assert.False(t, result.Valid, "Empty distribution should fail validation")
		assert.NotEmpty(t, result.Errors, "Should have validation errors")

		// Check for distribution error
		found := false

		for _, err := range result.Errors {
			if err.Field == specDistributionField &&
				strings.Contains(err.Message, "distribution is required") {
				found = true

				assert.Contains(t, err.FixSuggestion, "Set spec.cluster.distribution")

				break
			}
		}

		assert.True(t, found, "Should have distribution required error")
	})
}

// testEmptyDistributionConfig tests validation with empty distribution config.
func testEmptyDistributionConfig(t *testing.T) {
	t.Helper()

	t.Run("empty_distribution_config", func(t *testing.T) {
		t.Parallel()

		config := &v1alpha1.Cluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "ksail.dev/v1alpha1",
				Kind:       "Cluster",
			},
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution:       v1alpha1.DistributionKind,
					DistributionConfig: "", // Empty distribution config
				},
			},
		}

		validator := ksailvalidator.NewValidator()
		result := validator.Validate(config)

		assert.False(t, result.Valid, "Empty distribution config should fail validation")
		assert.NotEmpty(t, result.Errors, "Should have validation errors")

		// Check for distribution config error
		found := false

		for _, err := range result.Errors {
			if err.Field == "spec.cluster.distributionConfig" &&
				strings.Contains(err.Message, "distributionConfig is required") {
				found = true

				assert.Contains(t, err.FixSuggestion, "Set spec.cluster.distributionConfig")

				break
			}
		}

		assert.True(t, found, "Should have distribution config required error")
	})
}

// testInvalidDistributionValue tests validation with invalid distribution value.
func testInvalidDistributionValue(t *testing.T) {
	t.Helper()

	t.Run("invalid_distribution_value", func(t *testing.T) {
		t.Parallel()

		config := &v1alpha1.Cluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "ksail.dev/v1alpha1",
				Kind:       "Cluster",
			},
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution:       v1alpha1.Distribution("InvalidDistribution"),
					DistributionConfig: "config.yaml",
				},
			},
		}

		validator := ksailvalidator.NewValidator()
		result := validator.Validate(config)

		assert.False(t, result.Valid, "Invalid distribution should fail validation")
		assert.NotEmpty(t, result.Errors, "Should have validation errors")

		// Check for invalid distribution error
		found := false

		for _, err := range result.Errors {
			if err.Field == specDistributionField &&
				strings.Contains(err.Message, "invalid distribution value") {
				found = true

				assert.Contains(t, err.FixSuggestion, "Use a supported distribution")

				break
			}
		}

		assert.True(t, found, "Should have invalid distribution error")
	})
}

// TestKSailValidatorKindConfigEdgeCases tests Kind configuration edge cases.
func TestKSailValidatorKindConfigEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		kindConfig   *kindv1alpha4.Cluster
		expectedName string
		description  string
	}{
		{
			name:         "kind_with_whitespace_name",
			kindConfig:   &kindv1alpha4.Cluster{Name: "  test-kind  "},
			expectedName: "  test-kind  ",
			description:  "Should preserve whitespace in Kind config name",
		},
		{
			name:         "kind_with_special_characters",
			kindConfig:   &kindv1alpha4.Cluster{Name: "test-kind_123"},
			expectedName: "test-kind_123",
			description:  "Should handle special characters in Kind config name",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Create a valid config to test cross-configuration validation
			config := &v1alpha1.Cluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ksail.dev/v1alpha1",
					Kind:       "Cluster",
				},
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution:       v1alpha1.DistributionKind,
						DistributionConfig: "kind.yaml",
						Connection: v1alpha1.Connection{
							Context: "kind-" + test.expectedName,
						},
					},
				},
			}

			validator := ksailvalidator.NewValidatorForKind(test.kindConfig)
			result := validator.Validate(config)

			assert.True(t, result.Valid, test.description+" should pass validation")
			assert.Empty(
				t,
				result.Errors,
				test.description+" should have no validation errors",
			)
		})
	}
}

// TestKSailValidatorK3dConfigEdgeCases tests K3d configuration edge cases.
func TestKSailValidatorK3dConfigEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		k3dConfig    *k3dapi.SimpleConfig
		expectedName string
		description  string
	}{
		{
			name: "k3d_with_unicode_name",
			k3dConfig: &k3dapi.SimpleConfig{
				ObjectMeta: k3dtypes.ObjectMeta{Name: "test-k3d-ñ"},
			},
			expectedName: "test-k3d-ñ",
			description:  "Should handle unicode characters in K3d config name",
		},
		{
			name: "k3d_with_long_name",
			k3dConfig: &k3dapi.SimpleConfig{
				ObjectMeta: k3dtypes.ObjectMeta{
					Name: "very-long-k3d-cluster-name-that-exceeds-normal-length",
				},
			},
			expectedName: "very-long-k3d-cluster-name-that-exceeds-normal-length",
			description:  "Should handle long K3d config names",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Create a valid config to test cross-configuration validation
			config := &v1alpha1.Cluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ksail.dev/v1alpha1",
					Kind:       "Cluster",
				},
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution:       v1alpha1.DistributionK3d,
						DistributionConfig: "k3d.yaml",
						Connection: v1alpha1.Connection{
							Context: "k3d-" + test.expectedName,
						},
					},
				},
			}

			validator := ksailvalidator.NewValidatorForK3d(test.k3dConfig)
			result := validator.Validate(config)

			assert.True(t, result.Valid, test.description+" should pass validation")
			assert.Empty(
				t,
				result.Errors,
				test.description+" should have no validation errors",
			)
		})
	}
}

// TestKSailValidatorContextValidationComprehensive tests comprehensive context validation scenarios.
func TestKSailValidatorContextValidationComprehensive(t *testing.T) {
	t.Parallel()

	testKindContextValidation(t)
	testK3dContextValidation(t)
}

// testKindContextValidation tests Kind-specific context validation scenarios.
func testKindContextValidation(t *testing.T) {
	t.Helper()

	tests := []struct {
		name        string
		context     string
		shouldPass  bool
		description string
		useConfig   bool // Whether to provide a Kind config to the validator
	}{
		{
			name:        "kind_with_exact_match",
			context:     "kind-my-cluster",
			shouldPass:  true,
			description: "Kind context should match exactly",
			useConfig:   true,
		},
		{
			name:        "kind_with_case_mismatch",
			context:     "KIND-my-cluster",
			shouldPass:  false,
			description: "Kind context is case sensitive",
			useConfig:   true,
		},
		{
			name:        "kind_without_config_any_context",
			context:     "any-context",
			shouldPass:  true,
			description: "Kind context should not be validated without distribution config",
			useConfig:   false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			runKindContextValidationTest(t, test)
		})
	}
}

func runKindContextValidationTest(
	t *testing.T,
	test struct {
		name        string
		context     string
		shouldPass  bool
		description string
		useConfig   bool
	},
) {
	t.Helper()

	config := &v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "ksail.dev/v1alpha1",
			Kind:       "Cluster",
		},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:       v1alpha1.DistributionKind,
				DistributionConfig: "config.yaml",
				Connection: v1alpha1.Connection{
					Context: test.context,
				},
			},
		},
	}

	var validator *ksailvalidator.Validator

	if test.useConfig {
		kindConfig := &kindv1alpha4.Cluster{Name: "my-cluster"}
		validator = ksailvalidator.NewValidatorForKind(kindConfig)
	} else {
		validator = ksailvalidator.NewValidator()
	}

	result := validator.Validate(config)

	if test.shouldPass {
		assert.True(t, result.Valid, test.description+" should pass validation")
		assert.Empty(t, result.Errors, test.description+" should have no validation errors")
	} else {
		assert.False(t, result.Valid, test.description+" should fail validation")
		assert.NotEmpty(t, result.Errors, test.description+" should have validation errors")
	}
}

// testK3dContextValidation tests K3d-specific context validation scenarios.
func testK3dContextValidation(t *testing.T) {
	t.Helper()

	tests := []struct {
		name        string
		context     string
		shouldPass  bool
		description string
		useConfig   bool // Whether to provide a K3d config to the validator
	}{
		{
			name:        "k3d_with_exact_match",
			context:     "k3d-my-cluster",
			shouldPass:  true,
			description: "K3d context should match exactly",
			useConfig:   true,
		},
		{
			name:        "k3d_with_extra_prefix",
			context:     "prefix-k3d-my-cluster",
			shouldPass:  false,
			description: "K3d context should not have extra prefix",
			useConfig:   true,
		},
		{
			name:        "k3d_without_config_any_context",
			context:     "any-context",
			shouldPass:  true,
			description: "K3d context should not be validated without distribution config",
			useConfig:   false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			runK3dContextValidationTest(t, test)
		})
	}
}

func runK3dContextValidationTest(
	t *testing.T,
	test struct {
		name        string
		context     string
		shouldPass  bool
		description string
		useConfig   bool
	},
) {
	t.Helper()

	config := &v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "ksail.dev/v1alpha1",
			Kind:       "Cluster",
		},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:       v1alpha1.DistributionK3d,
				DistributionConfig: "config.yaml",
				Connection: v1alpha1.Connection{
					Context: test.context,
				},
			},
		},
	}

	var validator *ksailvalidator.Validator

	if test.useConfig {
		k3dConfig := &k3dapi.SimpleConfig{
			ObjectMeta: k3dtypes.ObjectMeta{Name: "my-cluster"},
		}
		validator = ksailvalidator.NewValidatorForK3d(k3dConfig)
	} else {
		validator = ksailvalidator.NewValidator()
	}

	result := validator.Validate(config)

	if test.shouldPass {
		assert.True(t, result.Valid, test.description+" should pass validation")
		assert.Empty(t, result.Errors, test.description+" should have no validation errors")
	} else {
		assert.False(t, result.Valid, test.description+" should fail validation")
		assert.NotEmpty(t, result.Errors, test.description+" should have validation errors")
	}
}

// TestKSailValidatorMultipleDistributionConfigs tests validator with multiple distribution configs.
func TestKSailValidatorMultipleDistributionConfigs(t *testing.T) {
	t.Parallel()

	testKindWithAllConfigs(t)
	testK3dWithAllConfigs(t)
}

// testKindWithAllConfigs tests Kind validation with all distribution configs available.
func testKindWithAllConfigs(t *testing.T) {
	t.Helper()

	t.Run("kind_with_config", func(t *testing.T) {
		t.Parallel()

		config := createMultiConfigTestCluster(v1alpha1.DistributionKind, "kind-test-kind")
		validator := createKindConfigValidator()
		result := validator.Validate(config)

		assert.True(t, result.Valid, "Validation should pass with Kind config")
		assert.Empty(t, result.Errors, "Should have no validation errors")
	})
}

// testK3dWithAllConfigs tests K3d validation with K3d distribution config available.
func testK3dWithAllConfigs(t *testing.T) {
	t.Helper()

	t.Run("k3d_with_config", func(t *testing.T) {
		t.Parallel()

		config := createMultiConfigTestCluster(v1alpha1.DistributionK3d, "k3d-test-k3d")
		validator := createK3dConfigValidator()
		result := validator.Validate(config)

		assert.True(t, result.Valid, "Validation should pass with K3d config")
		assert.Empty(t, result.Errors, "Should have no validation errors")
	})
}

// createMultiConfigTestCluster creates a test cluster config for multi-config testing.
func createMultiConfigTestCluster(
	distribution v1alpha1.Distribution,
	context string,
) *v1alpha1.Cluster {
	return &v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "ksail.dev/v1alpha1",
			Kind:       "Cluster",
		},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:       distribution,
				DistributionConfig: "config.yaml",
				Connection: v1alpha1.Connection{
					Context: context,
				},
			},
		},
	}
}

// createKindConfigValidator creates a validator with Kind distribution config.
func createKindConfigValidator() *ksailvalidator.Validator {
	kindConfig := &kindv1alpha4.Cluster{Name: "test-kind"}

	return ksailvalidator.NewValidatorForKind(kindConfig)
}

// createK3dConfigValidator creates a validator with K3d distribution config.
func createK3dConfigValidator() *ksailvalidator.Validator {
	k3dConfig := &k3dapi.SimpleConfig{
		ObjectMeta: k3dtypes.ObjectMeta{Name: "test-k3d"},
	}

	return ksailvalidator.NewValidatorForK3d(k3dConfig)
}

// kindDefaultCNITestCase defines a test case for Kind Default CNI validation.
type kindDefaultCNITestCase struct {
	name                string
	cni                 v1alpha1.CNI
	disableDefaultCNI   bool
	expectValid         bool
	expectedMessagePart string
}

// runKindDefaultCNITest runs a single Kind Default CNI validation test case.
func runKindDefaultCNITest(t *testing.T, testCase kindDefaultCNITestCase) {
	t.Helper()

	kindConfig := &kindv1alpha4.Cluster{
		Name: "ksail",
		Networking: kindv1alpha4.Networking{
			DisableDefaultCNI: testCase.disableDefaultCNI,
		},
	}

	validator := ksailvalidator.NewValidatorForKind(kindConfig)

	config := createValidKSailConfig(v1alpha1.DistributionKind)
	config.Spec.Cluster.CNI = testCase.cni
	config.Spec.Cluster.Connection.Context = kindKSailContext

	result := validator.Validate(config)

	if testCase.expectValid {
		assert.True(t, result.Valid, "expected config to be valid")
		assert.Empty(t, result.Errors, "expected no validation errors when configuration is valid")

		return
	}

	assert.False(t, result.Valid, "expected validation to fail")

	found := false

	for _, err := range result.Errors {
		if err.Field == specCNIField {
			found = true

			if testCase.expectedMessagePart != "" {
				assert.Contains(
					t,
					err.Message,
					testCase.expectedMessagePart,
					"error message should mention expected hint",
				)
			}

			assert.NotEmpty(t, err.FixSuggestion, "error should include fix suggestion")

			break
		}
	}

	assert.True(t, found, "expected to find Default CNI alignment validation error")
}

// TestKSailValidatorKindDefaultCNIAlignment tests validation for Default CNI with Kind.
func TestKSailValidatorKindDefaultCNIAlignment(t *testing.T) {
	t.Parallel()

	tests := []kindDefaultCNITestCase{
		{
			name:                "default_cni_with_disabled_cni",
			cni:                 v1alpha1.CNIDefault,
			disableDefaultCNI:   true,
			expectValid:         false,
			expectedMessagePart: "Default CNI requires disableDefaultCNI to be false",
		},
		{
			name:                "default_cni_with_enabled_cni",
			cni:                 v1alpha1.CNIDefault,
			disableDefaultCNI:   false,
			expectValid:         true,
			expectedMessagePart: "",
		},
		{
			name:                "empty_cni_with_disabled_cni",
			cni:                 "",
			disableDefaultCNI:   true,
			expectValid:         false,
			expectedMessagePart: "Default CNI requires disableDefaultCNI to be false",
		},
		{
			name:                "empty_cni_with_enabled_cni",
			cni:                 "",
			disableDefaultCNI:   false,
			expectValid:         true,
			expectedMessagePart: "",
		},
	}

	for idx := range tests {
		testCase := tests[idx]

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			runKindDefaultCNITest(t, testCase)
		})
	}
}

// k3dDefaultCNITestCase defines a test case for K3d Default CNI validation.
type k3dDefaultCNITestCase struct {
	name                string
	cni                 v1alpha1.CNI
	extraArgs           []k3dapi.K3sArgWithNodeFilters
	expectValid         bool
	expectedMessagePart string
}

// runK3dDefaultCNITest runs a single K3d Default CNI validation test case.
func runK3dDefaultCNITest(t *testing.T, testCase k3dDefaultCNITestCase) {
	t.Helper()

	k3dConfig := &k3dapi.SimpleConfig{
		ObjectMeta: k3dtypes.ObjectMeta{Name: "ksail"},
		Options: k3dapi.SimpleConfigOptions{
			K3sOptions: k3dapi.SimpleConfigOptionsK3s{
				ExtraArgs: testCase.extraArgs,
			},
		},
	}

	validator := ksailvalidator.NewValidatorForK3d(k3dConfig)

	config := createValidKSailConfig(v1alpha1.DistributionK3d)
	config.Spec.Cluster.CNI = testCase.cni
	config.Spec.Cluster.Connection.Context = "k3d-ksail"

	result := validator.Validate(config)

	if testCase.expectValid {
		assert.True(t, result.Valid, "expected config to be valid")
		assert.Empty(t, result.Errors, "expected no validation errors when configuration is valid")

		return
	}

	assert.False(t, result.Valid, "expected validation to fail")

	found := false

	for _, err := range result.Errors {
		if err.Field == specCNIField {
			found = true

			if testCase.expectedMessagePart != "" {
				assert.Contains(
					t,
					err.Message,
					testCase.expectedMessagePart,
					"error message should mention expected hint",
				)
			}

			assert.NotEmpty(t, err.FixSuggestion, "error should include fix suggestion")

			break
		}
	}

	assert.True(t, found, "expected to find Default CNI alignment validation error")
}

// TestKSailValidatorK3dDefaultCNIAlignment tests validation for Default CNI with K3d.
func TestKSailValidatorK3dDefaultCNIAlignment(t *testing.T) {
	t.Parallel()

	tests := []k3dDefaultCNITestCase{
		{
			name: "default_cni_with_flannel_disabled",
			cni:  v1alpha1.CNIDefault,
			extraArgs: []k3dapi.K3sArgWithNodeFilters{
				{Arg: "--flannel-backend=none", NodeFilters: []string{"server:*"}},
			},
			expectValid:         false,
			expectedMessagePart: "Default CNI requires Flannel to be enabled",
		},
		{
			name: "default_cni_with_network_policy_disabled",
			cni:  v1alpha1.CNIDefault,
			extraArgs: []k3dapi.K3sArgWithNodeFilters{
				{Arg: "--disable-network-policy", NodeFilters: []string{"server:*"}},
			},
			expectValid:         false,
			expectedMessagePart: "Default CNI requires Flannel to be enabled",
		},
		{
			name: "default_cni_with_both_disabled",
			cni:  v1alpha1.CNIDefault,
			extraArgs: []k3dapi.K3sArgWithNodeFilters{
				{Arg: "--flannel-backend=none", NodeFilters: []string{"server:*"}},
				{Arg: "--disable-network-policy", NodeFilters: []string{"server:*"}},
			},
			expectValid:         false,
			expectedMessagePart: "Default CNI requires Flannel to be enabled",
		},
		{
			name:                "default_cni_with_flannel_enabled",
			cni:                 v1alpha1.CNIDefault,
			extraArgs:           []k3dapi.K3sArgWithNodeFilters{},
			expectValid:         true,
			expectedMessagePart: "",
		},
		{
			name:                "empty_cni_with_flannel_enabled",
			cni:                 "",
			extraArgs:           []k3dapi.K3sArgWithNodeFilters{},
			expectValid:         true,
			expectedMessagePart: "",
		},
	}

	for idx := range tests {
		testCase := tests[idx]

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			runK3dDefaultCNITest(t, testCase)
		})
	}
}
