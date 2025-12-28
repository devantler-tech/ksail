package configmanager_test

import (
	"io"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	configmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// flagNameTestCase represents a test case for flag name generation.
type flagNameTestCase struct {
	name     string
	fieldPtr any
	expected string
}

type fieldTestCase struct {
	name          string
	fieldSelector configmanager.FieldSelector[v1alpha1.Cluster]
	expectedFlag  string
	expectedType  string
}

// runFlagNameGenerationTests is a helper function to run multiple flag name generation test cases.
func runFlagNameGenerationTests(
	t *testing.T,
	manager *configmanager.ConfigManager,
	tests []flagNameTestCase,
) {
	t.Helper()
	t.Helper()

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			testFlagNameGeneration(t, manager, testCase.fieldPtr, testCase.expected)
		})
	}
}

// setupFlagBindingTest creates a command for testing flag binding.
func setupFlagBindingTest(
	fieldSelectors ...configmanager.FieldSelector[v1alpha1.Cluster],
) *cobra.Command {
	manager := configmanager.NewConfigManager(io.Discard, fieldSelectors...)
	cmd := &cobra.Command{Use: "test"}
	manager.AddFlagsFromFields(cmd)

	return cmd
}

// getBasicFieldTests returns test cases for basic field testing.
func getBasicFieldTests() []fieldTestCase {
	return []fieldTestCase{
		newDistributionFieldTest(),
		newSourceDirectoryFieldTest(),
		newGitOpsEngineFieldTest(),
		newLocalRegistryFieldTest(),
		newRegistryPortFieldTest(),
		newFluxIntervalFieldTest(),
	}
}

func newDistributionFieldTest() fieldTestCase {
	return fieldTestCase{
		name: "Distribution field",
		fieldSelector: newFieldSelector(
			func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.Distribution },
			v1alpha1.DistributionKind,
			"Kubernetes distribution",
		),
		expectedFlag: "distribution",
		expectedType: "Distribution",
	}
}

func newSourceDirectoryFieldTest() fieldTestCase {
	return fieldTestCase{
		name: "SourceDirectory field",
		fieldSelector: newFieldSelector(
			func(c *v1alpha1.Cluster) any { return &c.Spec.Workload.SourceDirectory },
			"k8s",
			"Source directory",
		),
		expectedFlag: "source-directory",
		expectedType: "string",
	}
}

func newGitOpsEngineFieldTest() fieldTestCase {
	return fieldTestCase{
		name: "GitOpsEngine field",
		fieldSelector: newFieldSelector(
			func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.GitOpsEngine },
			v1alpha1.GitOpsEngineNone,
			"GitOps engine",
		),
		expectedFlag: "gitops-engine",
		expectedType: "GitOpsEngine",
	}
}

func newLocalRegistryFieldTest() fieldTestCase {
	return fieldTestCase{
		name: "LocalRegistry field",
		fieldSelector: newFieldSelector(
			func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.LocalRegistry },
			v1alpha1.LocalRegistryDisabled,
			"Local registry",
		),
		expectedFlag: "local-registry",
		expectedType: "LocalRegistry",
	}
}

func newRegistryPortFieldTest() fieldTestCase {
	return fieldTestCase{
		name: "RegistryPort field",
		fieldSelector: newFieldSelector(
			func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.LocalRegistryOpts.HostPort },
			int32(5000),
			"Registry port",
		),
		expectedFlag: "local-registry-port",
		expectedType: "int32",
	}
}

func newFluxIntervalFieldTest() fieldTestCase {
	return fieldTestCase{
		name: "FluxInterval field",
		fieldSelector: newFieldSelector(
			func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.Flux.Interval },
			metav1.Duration{Duration: time.Minute},
			"Flux interval",
		),
		expectedFlag: "flux-interval",
		expectedType: "duration",
	}
}

func TestAddFlagFromField(t *testing.T) {
	t.Parallel()

	t.Run("basic fields", func(t *testing.T) {
		t.Parallel()
		testAddFlagFromFieldCases(t, getBasicFieldTests())
	})

	t.Run("connection fields", func(t *testing.T) {
		t.Parallel()
		testAddFlagFromFieldCases(t, getConnectionFieldTests())
	})

	t.Run("networking fields", func(t *testing.T) {
		t.Parallel()
		testAddFlagFromFieldCases(t, getNetworkingFieldTests())
	})

	t.Run("error handling", func(t *testing.T) {
		t.Parallel()
		testAddFlagFromFieldErrorHandling(t)
	})
}

// getConnectionFieldTests returns test cases for connection field testing.
func getConnectionFieldTests() []fieldTestCase {
	return []fieldTestCase{
		{
			name: "Context field",
			fieldSelector: newFieldSelector(
				func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.Connection.Context },
				"",
				"Kubernetes context",
			),
			expectedFlag: "context",
			expectedType: "string",
		},
		{
			name: "Timeout field",
			fieldSelector: newFieldSelector(
				func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.Connection.Timeout },
				metav1.Duration{Duration: 5 * time.Minute},
				"Connection timeout",
			),
			expectedFlag: "timeout",
			expectedType: "duration",
		},
	}
}

// getNetworkingFieldTests returns test cases for networking field testing.
func getNetworkingFieldTests() []fieldTestCase {
	return []fieldTestCase{
		{
			name: "CNI field",
			fieldSelector: newFieldSelector(
				func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.CNI },
				v1alpha1.CNICilium,
				"CNI plugin",
			),
			expectedFlag: "cni",
			expectedType: "CNI",
		},
		{
			name: "CSI field",
			fieldSelector: newFieldSelector(
				func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.CSI },
				v1alpha1.CSILocalPathStorage,
				"CSI driver",
			),
			expectedFlag: "csi",
			expectedType: "CSI",
		},
		{
			name: "MetricsServer field",
			fieldSelector: newFieldSelector(
				func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.MetricsServer },
				v1alpha1.MetricsServerEnabled,
				"Metrics Server configuration",
			),
			expectedFlag: "metrics-server",
			expectedType: "MetricsServer",
		},
		{
			name: "CertManager field",
			fieldSelector: newFieldSelector(
				func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.CertManager },
				v1alpha1.CertManagerDisabled,
				"Cert-Manager configuration",
			),
			expectedFlag: "cert-manager",
			expectedType: "CertManager",
		},
	}
}

// testAddFlagFromFieldErrorHandling tests error handling scenarios for AddFlagFromField.
func testAddFlagFromFieldErrorHandling(t *testing.T) {
	t.Helper()

	tests := []struct {
		name          string
		fieldSelector configmanager.FieldSelector[v1alpha1.Cluster]
		expectSkip    bool
	}{
		{
			name: "Nil field selector",
			fieldSelector: configmanager.FieldSelector[v1alpha1.Cluster]{
				Selector: func(_ *v1alpha1.Cluster) any { return nil },
			},
			expectSkip: true,
		},
		{
			name: "Valid field selector",
			fieldSelector: newFieldSelector(
				func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.Distribution },
				"test",
				"Test field",
			),
			expectSkip: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cmd := setupFlagBindingTest(testCase.fieldSelector)

			if testCase.expectSkip {
				// Should have no flags when selector returns nil
				assert.False(t, cmd.Flags().HasFlags())
			} else {
				// Should have flags when selector is valid
				assert.True(t, cmd.Flags().HasFlags())
			}
		})
	}
}

// testAddFlagFromFieldCases is a helper function to test field selector functionality.
func testAddFlagFromFieldCases(t *testing.T, tests []fieldTestCase,
) {
	t.Helper()

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cmd := setupFlagBindingTest(testCase.fieldSelector)

			// Check that the flag was added
			flag := cmd.Flags().Lookup(testCase.expectedFlag)
			require.NotNil(t, flag, "flag %s should exist", testCase.expectedFlag)
			assert.Equal(t, testCase.fieldSelector.Description, flag.Usage)

			// Check flag type
			assert.Equal(t, testCase.expectedType, flag.Value.Type())
		})
	}
}

// TestGenerateFlagName tests flag name generation for various field types.
func TestGenerateFlagName(t *testing.T) {
	t.Parallel()

	manager := configmanager.NewConfigManager(io.Discard)

	tests := []flagNameTestCase{
		{"Distribution field", &manager.Config.Spec.Cluster.Distribution, "distribution"},
		{
			"DistributionConfig field",
			&manager.Config.Spec.Cluster.DistributionConfig,
			"distribution-config",
		},
		{
			"SourceDirectory field",
			&manager.Config.Spec.Workload.SourceDirectory,
			"source-directory",
		},
		{
			"GitOpsEngine field",
			&manager.Config.Spec.Cluster.GitOpsEngine,
			"gitops-engine",
		},
		{"Context field", &manager.Config.Spec.Cluster.Connection.Context, "context"},
		{"Kubeconfig field", &manager.Config.Spec.Cluster.Connection.Kubeconfig, "kubeconfig"},
		{"Timeout field", &manager.Config.Spec.Cluster.Connection.Timeout, "timeout"},
		{"CNI field", &manager.Config.Spec.Cluster.CNI, "cni"},
		{"CSI field", &manager.Config.Spec.Cluster.CSI, "csi"},
		{
			"MetricsServer field",
			&manager.Config.Spec.Cluster.MetricsServer,
			"metrics-server",
		},
		{
			"LocalRegistry field",
			&manager.Config.Spec.Cluster.LocalRegistry,
			"local-registry",
		},
		{
			"RegistryPort field",
			&manager.Config.Spec.Cluster.LocalRegistryOpts.HostPort,
			"local-registry-port",
		},
		{
			"FluxInterval field",
			&manager.Config.Spec.Cluster.Flux.Interval,
			"flux-interval",
		},
	}

	runFlagNameGenerationTests(t, manager, tests)
}

// testFlagNameGeneration is a helper function to test flag name generation.
func testFlagNameGeneration(
	t *testing.T,
	manager *configmanager.ConfigManager,
	fieldPtr any,
	expected string,
) {
	t.Helper()

	result := manager.GenerateFlagName(fieldPtr)
	assert.Equal(t, expected, result)
}

// TestManager_GenerateShorthand tests the GenerateShorthand method.
func TestGenerateShorthand(t *testing.T) {
	t.Parallel()

	manager := configmanager.NewConfigManager(io.Discard)

	tests := []struct {
		name     string
		flagName string
		expected string
	}{
		{
			name:     "distribution flag",
			flagName: "distribution",
			expected: "d",
		},
		{
			name:     "context flag",
			flagName: "context",
			expected: "c",
		},
		{
			name:     "kubeconfig flag",
			flagName: "kubeconfig",
			expected: "k",
		},
		{
			name:     "timeout flag",
			flagName: "timeout",
			expected: "t",
		},
		{
			name:     "source-directory flag",
			flagName: "source-directory",
			expected: "s",
		},
		{
			name:     "gitops-engine flag",
			flagName: "gitops-engine",
			expected: "g",
		},
		{
			name:     "distribution-config flag (no shorthand)",
			flagName: "distribution-config",
			expected: "",
		},
		{
			name:     "unknown flag (no shorthand)",
			flagName: "unknown-flag",
			expected: "",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// Call the public method directly
			result := manager.GenerateShorthand(testCase.flagName)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestAddFlagsFromFields_GitOpsEngineAcceptsArgoCD(t *testing.T) {
	t.Parallel()

	gitOpsEngineSelector := newGitOpsEngineFieldTest().fieldSelector
	manager := configmanager.NewConfigManager(io.Discard, gitOpsEngineSelector)
	cmd := &cobra.Command{Use: "test"}
	manager.AddFlagsFromFields(cmd)

	require.NoError(t, cmd.Flags().Set("gitops-engine", "ArgoCD"))
	assert.Equal(t, v1alpha1.GitOpsEngine("ArgoCD"), manager.Config.Spec.Cluster.GitOpsEngine)
}
