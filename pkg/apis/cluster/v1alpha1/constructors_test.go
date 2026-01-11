package v1alpha1_test

import (
	"testing"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestClusterDirectCreation(t *testing.T) {
	t.Parallel()

	// Test direct cluster creation without constructors
	cluster := &v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       v1alpha1.Kind,
			APIVersion: v1alpha1.APIVersion,
		},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionK3s,
				Connection: v1alpha1.Connection{
					Kubeconfig: "/test",
					Context:    "test-ctx",
					Timeout:    metav1.Duration{Duration: time.Duration(10) * time.Minute},
				},
				CNI:          v1alpha1.CNICilium,
				CSI:          v1alpha1.CSILocalPathStorage,
				GitOpsEngine: v1alpha1.GitOpsEngineNone,
			},
		},
	}

	assert.Equal(t, v1alpha1.Kind, cluster.Kind)
	assert.Equal(t, v1alpha1.APIVersion, cluster.APIVersion)
	assert.Equal(t, v1alpha1.DistributionK3s, cluster.Spec.Cluster.Distribution)
}

func TestDistributionSet(t *testing.T) {
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

func TestDistributionIsValid(t *testing.T) {
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

func TestGitOpsEngineSet(t *testing.T) {
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

func TestCNISet(t *testing.T) {
	t.Parallel()

	validCases := []struct{ input, expected string }{
		{"Default", "Default"},
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

func TestCSISet(t *testing.T) {
	t.Parallel()

	validCases := []struct{ input, expected string }{
		{"Default", "Default"},
		{"localpathstorage", "LocalPathStorage"},
		{"LOCALPATHSTORAGE", "LocalPathStorage"},
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

func TestMetricsServerSet(t *testing.T) {
	t.Parallel()

	validCases := []struct{ input, expected string }{
		{"Default", "Default"},
		{"default", "Default"},
		{"DEFAULT", "Default"},
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

func TestCertManagerSet(t *testing.T) {
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
	assert.Equal(t, "Default", cni.String())
	assert.Equal(t, "CNI", cni.Type())

	csi := v1alpha1.CSIDefault
	assert.Equal(t, "Default", csi.String())
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

// Tests for constructor functions

func TestNewCluster(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.NewCluster()

	require.NotNil(t, cluster)
	assert.Equal(t, v1alpha1.Kind, cluster.Kind)
	assert.Equal(t, v1alpha1.APIVersion, cluster.APIVersion)
	assert.NotNil(t, cluster.Spec)
}

func TestNewClusterSpec(t *testing.T) {
	t.Parallel()

	spec := v1alpha1.NewClusterSpec()

	assert.Empty(t, spec.Cluster.DistributionConfig)
	assert.Empty(t, spec.Workload.SourceDirectory)
	assert.NotNil(t, spec.Cluster.Connection)
	assert.Equal(t, v1alpha1.Distribution(""), spec.Cluster.Distribution)
	assert.Equal(t, v1alpha1.CNI(""), spec.Cluster.CNI)
	assert.Equal(t, v1alpha1.CSI(""), spec.Cluster.CSI)
	assert.Equal(t, v1alpha1.GitOpsEngineNone, spec.Cluster.GitOpsEngine)
}

func TestNewClusterConnection(t *testing.T) {
	t.Parallel()

	connection := v1alpha1.NewClusterConnection()

	assert.Empty(t, connection.Kubeconfig)
	assert.Empty(t, connection.Context)
	assert.Equal(t, metav1.Duration{Duration: 0}, connection.Timeout)
}

// Tests for individual option constructors

func TestNewClusterOptionsVanilla(t *testing.T) {
	t.Parallel()

	options := v1alpha1.NewClusterOptionsVanilla()

	// OptionsVanilla is an empty struct, just verify it's created
	assert.NotNil(t, options)
}

func TestNewLocalRegistry(t *testing.T) {
	t.Parallel()

	options := v1alpha1.NewLocalRegistry()

	// LocalRegistry is an empty struct, just verify it's created
	assert.NotNil(t, options)
}

func TestNewOCIRegistry(t *testing.T) {
	t.Parallel()

	registry := v1alpha1.NewOCIRegistry()

	assert.Equal(t, v1alpha1.OCIRegistryStatusNotProvisioned, registry.Status)
	assert.Empty(t, registry.Endpoint)
}
