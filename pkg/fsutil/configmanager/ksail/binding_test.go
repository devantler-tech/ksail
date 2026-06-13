package configmanager_test

import (
	"io"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type fieldTestCase struct {
	name          string
	fieldSelector configmanager.FieldSelector[v1alpha1.Cluster]
	expectedFlag  string
	expectedType  string
}

// setupFlagBindingTest creates a command for testing flag binding.
func setupFlagBindingTest(
	fieldSelectors ...configmanager.FieldSelector[v1alpha1.Cluster],
) *cobra.Command {
	manager := configmanager.NewConfigManager(io.Discard, "", fieldSelectors...)
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
			"distribution",
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
			"source-directory",
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
			"gitops-engine",
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
			"local-registry",
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
				"context",
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
				"timeout",
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
				"cni",
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
				"csi",
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
				"metrics-server",
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
				"load-balancer",
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
				"cert-manager",
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
				"distribution",
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

// TestFieldSelectorFlagMetadata pins the FlagName/Shorthand carried by the
// representative Default* field-selector constructors. The flag name and
// shorthand now live on the FieldSelector itself (replacing the deleted
// pointer-identity GenerateFlagName/GenerateShorthand maps), so registration is
// byte-identical to the previous behavior.
func TestFieldSelectorFlagMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		selector  configmanager.FieldSelector[v1alpha1.Cluster]
		flagName  string
		shorthand string
	}{
		{"distribution", configmanager.DefaultDistributionFieldSelector(), "distribution", "d"},
		// DefaultProviderFieldSelector carries no shorthand; the mutation commands
		// add -p via WithProviderShorthand (asserted separately below).
		{"provider", configmanager.DefaultProviderFieldSelector(), "provider", ""},
		{
			"provider-with-shorthand",
			configmanager.WithProviderShorthand(configmanager.DefaultProviderFieldSelector()),
			"provider",
			"p",
		},
		{"context", configmanager.DefaultContextFieldSelector(), "context", "c"},
		{"kubeconfig", configmanager.DefaultKubeconfigFieldSelector(), "kubeconfig", "k"},
		{
			"source-directory",
			configmanager.StandardSourceDirectoryFieldSelector(),
			"source-directory",
			"s",
		},
		{"gitops-engine", configmanager.DefaultGitOpsEngineFieldSelector(), "gitops-engine", "g"},
		{
			"distribution-config",
			configmanager.DefaultDistributionConfigFieldSelector(),
			"distribution-config",
			"",
		},
		{"cni", configmanager.DefaultCNIFieldSelector(), "cni", ""},
		{"local-registry", configmanager.DefaultLocalRegistryFieldSelector(), "local-registry", ""},
		{"drain-timeout", configmanager.DrainTimeoutFieldSelector(), "drain-timeout", ""},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.flagName, testCase.selector.FlagName)
			assert.Equal(t, testCase.shorthand, testCase.selector.Shorthand)
		})
	}
}

// TestAddFlagsFromFields_PanicsOnMissingFlagName asserts that registering a
// field selector without a FlagName is a programming error caught at init time —
// it can no longer silently register a flag literally named "unknown".
func TestAddFlagsFromFields_PanicsOnMissingFlagName(t *testing.T) {
	t.Parallel()

	manager := configmanager.NewConfigManager(
		io.Discard,
		"",
		configmanager.FieldSelector[v1alpha1.Cluster]{
			Selector:    func(c *v1alpha1.Cluster) any { return &c.Spec.Cluster.Distribution },
			Description: "missing flag name",
		},
	)
	cmd := &cobra.Command{Use: "test"}

	assert.PanicsWithError(t, configmanager.ErrFieldSelectorMissingFlagName.Error()+
		` (description: "missing flag name")`, func() {
		manager.AddFlagsFromFields(cmd)
	})
}

// TestAllClusterFieldSelectorsHaveFlagName guards against a future constructor
// forgetting to set FlagName: registering every selector used by the mutation
// commands must not panic.
func TestAllClusterFieldSelectorsHaveFlagName(t *testing.T) {
	t.Parallel()

	selectors := []configmanager.FieldSelector[v1alpha1.Cluster]{
		configmanager.DefaultProviderFieldSelector(),
		configmanager.DefaultCNIFieldSelector(),
		configmanager.DefaultMetricsServerFieldSelector(),
		configmanager.DefaultLoadBalancerFieldSelector(),
		configmanager.DefaultCertManagerFieldSelector(),
		configmanager.DefaultPolicyEngineFieldSelector(),
		configmanager.DefaultCSIFieldSelector(),
		configmanager.DefaultCDIFieldSelector(),
		configmanager.DefaultImportImagesFieldSelector(),
		configmanager.KubernetesVersionFieldSelector(),
		configmanager.DistributionVersionFieldSelector(),
		configmanager.DrainTimeoutFieldSelector(),
		configmanager.ControlPlanesFieldSelector(),
		configmanager.WorkersFieldSelector(),
		configmanager.NodeAutoscalerEnabledFieldSelector(),
		configmanager.ImageVerificationFieldSelector(),
		configmanager.OIDCIssuerURLFieldSelector(),
		configmanager.OIDCClientIDFieldSelector(),
		configmanager.OIDCUsernameClaimFieldSelector(),
		configmanager.OIDCUsernamePrefixFieldSelector(),
		configmanager.OIDCGroupsClaimFieldSelector(),
		configmanager.OIDCGroupsPrefixFieldSelector(),
		configmanager.OIDCCAFileFieldSelector(),
	}

	for _, selector := range selectors {
		assert.NotEmpty(t, selector.FlagName, "every field selector must set FlagName")
	}

	allClusterSelectors := append(configmanager.DefaultClusterFieldSelectors(), selectors...)
	manager := configmanager.NewConfigManager(io.Discard, "", allClusterSelectors...)
	cmd := &cobra.Command{Use: "test"}

	assert.NotPanics(t, func() { manager.AddFlagsFromFields(cmd) })
}

func TestAddFlagsFromFields_GitOpsEngineAcceptsArgoCD(t *testing.T) {
	t.Parallel()

	gitOpsEngineSelector := newGitOpsEngineFieldTest().fieldSelector
	manager := configmanager.NewConfigManager(io.Discard, "", gitOpsEngineSelector)
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
				"test-bool",
				func(c *v1alpha1.Cluster) any {
					return &c.Spec.Provider.Hetzner.PlacementGroupFallbackToNone
				},
				testCase.defaultValue,
				"test bool field",
			)

			assertFlagRegistered(t, selector, "test-bool", "bool", testCase.setValue)

			manager := configmanager.NewConfigManager(io.Discard, "", selector)
			cmd := &cobra.Command{Use: "test"}
			manager.AddFlagsFromFields(cmd)

			require.NoError(t, cmd.Flags().Set("test-bool", testCase.setValue))
			assert.Equal(
				t, testCase.expected,
				manager.Config.Spec.Provider.Hetzner.PlacementGroupFallbackToNone,
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
				"control-planes",
				func(c *v1alpha1.Cluster) any {
					return &c.Spec.Cluster.ControlPlanes
				},
				testCase.defaultValue,
				"Number of control planes",
			)

			assertFlagRegistered(t, selector, "control-planes", "int32", testCase.setValue)

			manager := configmanager.NewConfigManager(io.Discard, "", selector)
			cmd := &cobra.Command{Use: "test"}
			manager.AddFlagsFromFields(cmd)

			require.NoError(t, cmd.Flags().Set("control-planes", testCase.setValue))
			assert.Equal(
				t, testCase.expected,
				manager.Config.Spec.Cluster.ControlPlanes,
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

	manager := configmanager.NewConfigManager(io.Discard, "", selector)
	cmd := &cobra.Command{Use: "test"}
	manager.AddFlagsFromFields(cmd)

	flag := cmd.Flags().Lookup(flagName)
	require.NotNil(t, flag, "flag %q should be registered", flagName)
	assert.Equal(t, expectedType, flag.Value.Type())

	require.NoError(t, cmd.Flags().Set(flagName, setValue))
}
