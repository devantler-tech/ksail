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
	}
}

func newDistributionFieldTest() fieldTestCase {
	return fieldTestCase{
		name: "Distribution field",
		fieldSelector: newFieldSelector(
			func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.Distribution },
			v1alpha1.DistributionVanilla,
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
		name: "LocalRegistry.Registry field",
		fieldSelector: newFieldSelector(
			func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.LocalRegistry.Registry },
			"",
			"Local registry",
		),
		expectedFlag: "local-registry",
		expectedType: "string",
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
				v1alpha1.CSIEnabled,
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
			name: "LoadBalancer field",
			fieldSelector: newFieldSelector(
				func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.LoadBalancer },
				v1alpha1.LoadBalancerDefault,
				"LoadBalancer configuration",
			),
			expectedFlag: "load-balancer",
			expectedType: "LoadBalancer",
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
			"LoadBalancer field",
			&manager.Config.Spec.Cluster.LoadBalancer,
			"load-balancer",
		},
		{
			"LocalRegistry.Registry field",
			&manager.Config.Spec.Cluster.LocalRegistry.Registry,
			"local-registry",
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

func TestAddFlagsFromFields_BoolField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		defaultValue any
		setValue     string
		expected     bool
	}{
		{
			name:         "bool with default false",
			defaultValue: false,
			setValue:     "true",
			expected:     true,
		},
		{
			name:         "bool with default true",
			defaultValue: true,
			setValue:     "false",
			expected:     false,
		},
		{
			name:         "bool with nil default",
			defaultValue: nil,
			setValue:     "true",
			expected:     true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			selector := newFieldSelector(
				func(c *v1alpha1.Cluster) any {
					return &c.Spec.Cluster.Hetzner.PlacementGroupFallbackToNone
				},
				testCase.defaultValue,
				"test bool field",
			)

			assertFlagRegistered(t, selector, "unknown", "bool", testCase.setValue)

			manager := configmanager.NewConfigManager(io.Discard, selector)
			cmd := &cobra.Command{Use: "test"}
			manager.AddFlagsFromFields(cmd)

			require.NoError(t, cmd.Flags().Set("unknown", testCase.setValue))
			assert.Equal(
				t, testCase.expected,
				manager.Config.Spec.Cluster.Hetzner.PlacementGroupFallbackToNone,
			)
		})
	}
}

func TestAddFlagsFromFields_Int32Field(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		defaultValue any
		setValue     string
		expected     int32
	}{
		{
			name:         "int32 with no default",
			defaultValue: nil,
			setValue:     "5",
			expected:     5,
		},
		{
			name:         "int32 with default value",
			defaultValue: int32(3),
			setValue:     "7",
			expected:     7,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			selector := newFieldSelector(
				func(c *v1alpha1.Cluster) any {
					return &c.Spec.Cluster.Talos.ControlPlanes
				},
				testCase.defaultValue,
				"Number of control planes",
			)

			assertFlagRegistered(t, selector, "control-planes", "int32", testCase.setValue)

			manager := configmanager.NewConfigManager(io.Discard, selector)
			cmd := &cobra.Command{Use: "test"}
			manager.AddFlagsFromFields(cmd)

			require.NoError(t, cmd.Flags().Set("control-planes", testCase.setValue))
			assert.Equal(
				t, testCase.expected,
				manager.Config.Spec.Cluster.Talos.ControlPlanes,
			)
		})
	}
}

// assertFlagRegistered verifies that a field selector registers a flag with the expected name and type.
func assertFlagRegistered(
	t *testing.T,
	selector configmanager.FieldSelector[v1alpha1.Cluster],
	flagName string,
	expectedType string,
	setValue string,
) {
	t.Helper()

	manager := configmanager.NewConfigManager(io.Discard, selector)
	cmd := &cobra.Command{Use: "test"}
	manager.AddFlagsFromFields(cmd)

	flag := cmd.Flags().Lookup(flagName)
	require.NotNil(t, flag, "flag %q should be registered", flagName)
	assert.Equal(t, expectedType, flag.Value.Type())

	require.NoError(t, cmd.Flags().Set(flagName, setValue))
}
