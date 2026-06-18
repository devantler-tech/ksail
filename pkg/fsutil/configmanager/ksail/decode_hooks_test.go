package configmanager_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	configmanagerinterface "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadCertManagerConfig writes a ksail.yaml with the given certManager value and
// loads it through the config manager, returning the resolved CertManager value.
func loadCertManagerConfig(t *testing.T, certManagerValue string) v1alpha1.CertManager {
	t.Helper()

	tmpDir := t.TempDir()
	configContent := `apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: test-toggle
spec:
  cluster:
    distribution: Vanilla
    provider: Docker
    certManager: ` + certManagerValue + `
    connection:
      context: kind-test-toggle
      kubeconfig: "~/.kube/config"
`
	configPath := filepath.Join(tmpDir, "ksail.yaml")
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	mgr := configmanager.NewConfigManager(nil, configPath)

	cluster, err := mgr.Load(configmanagerinterface.LoadOptions{
		SkipValidation: true,
		Silent:         true,
	})
	require.NoError(t, err)
	require.NotNil(t, cluster)

	return cluster.Spec.Cluster.CertManager
}

// TestConfigManager_ToggleBoolAlias verifies the toggleBoolAliasDecodeHook: a
// boolean value for a toggle-enum field (certManager) is coerced to its
// canonical Enabled/Disabled string, while the long-standing string spelling
// still loads unchanged.
func TestConfigManager_ToggleBoolAlias(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    string
		expected v1alpha1.CertManager
	}{
		{
			name:     "bool true coerces to Enabled",
			value:    "true",
			expected: v1alpha1.CertManagerEnabled,
		},
		{
			name:     "string Enabled still loads",
			value:    "Enabled",
			expected: v1alpha1.CertManagerEnabled,
		},
		{
			name:     "bool false coerces to Disabled",
			value:    "false",
			expected: v1alpha1.CertManagerDisabled,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := loadCertManagerConfig(t, testCase.value)
			assert.Equal(t, testCase.expected, got)
		})
	}
}

// loadSOPSEnabledConfig writes a ksail.yaml with the given sops.enabled value and
// loads it, returning the resolved SOPSEnabled value.
func loadSOPSEnabledConfig(t *testing.T, enabledValue string) v1alpha1.SOPSEnabled {
	t.Helper()

	tmpDir := t.TempDir()
	configContent := `apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: test-sops-toggle
spec:
  cluster:
    distribution: Vanilla
    provider: Docker
    sops:
      enabled: ` + enabledValue + `
`
	configPath := filepath.Join(tmpDir, "ksail.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0o600))

	mgr := configmanager.NewConfigManager(nil, configPath)

	cluster, err := mgr.Load(configmanagerinterface.LoadOptions{SkipValidation: true, Silent: true})
	require.NoError(t, err)
	require.NotNil(t, cluster)

	return cluster.Spec.Cluster.SOPS.Enabled
}

// TestConfigManager_SOPSEnabledBoolAlias verifies that the SOPSEnabled tri-state
// toggle accepts the legacy YAML boolean (sops.enabled: true|false) — coercing it
// to Enabled/Disabled — while the new string spellings (Enabled/Disabled/Default)
// also load unchanged, so existing ksail.yaml files keep working.
func TestConfigManager_SOPSEnabledBoolAlias(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    string
		expected v1alpha1.SOPSEnabled
	}{
		{"legacy bool true coerces to Enabled", "true", v1alpha1.SOPSEnabledEnabled},
		{"legacy bool false coerces to Disabled", "false", v1alpha1.SOPSEnabledDisabled},
		{"string Enabled loads", "Enabled", v1alpha1.SOPSEnabledEnabled},
		{"string Disabled loads", "Disabled", v1alpha1.SOPSEnabledDisabled},
		{"string Default loads", "Default", v1alpha1.SOPSEnabledDefault},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.expected, loadSOPSEnabledConfig(t, testCase.value))
		})
	}
}

// loadNodeAutoscalerEnabledConfig writes a ksail.yaml with the given
// autoscaler.node.enabled value and loads it, returning the resolved value.
func loadNodeAutoscalerEnabledConfig(
	t *testing.T,
	enabledValue string,
) v1alpha1.NodeAutoscalerEnabled {
	t.Helper()

	tmpDir := t.TempDir()
	configContent := `apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: test-autoscaler-toggle
spec:
  cluster:
    distribution: Vanilla
    provider: Docker
    autoscaler:
      node:
        enabled: ` + enabledValue + `
`
	configPath := filepath.Join(tmpDir, "ksail.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0o600))

	mgr := configmanager.NewConfigManager(nil, configPath)

	cluster, err := mgr.Load(configmanagerinterface.LoadOptions{SkipValidation: true, Silent: true})
	require.NoError(t, err)
	require.NotNil(t, cluster)

	return cluster.Spec.Cluster.Autoscaler.Node.Enabled
}

// TestConfigManager_NodeAutoscalerEnabledBoolAlias verifies that the
// NodeAutoscalerEnabled toggle accepts the legacy YAML boolean
// (autoscaler.node.enabled: true|false) while the new string spellings also load,
// so existing ksail.yaml files keep working.
func TestConfigManager_NodeAutoscalerEnabledBoolAlias(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    string
		expected v1alpha1.NodeAutoscalerEnabled
	}{
		{"legacy bool true coerces to Enabled", "true", v1alpha1.NodeAutoscalerEnabledEnabled},
		{"legacy bool false coerces to Disabled", "false", v1alpha1.NodeAutoscalerEnabledDisabled},
		{"string Enabled loads", "Enabled", v1alpha1.NodeAutoscalerEnabledEnabled},
		{"string Disabled loads", "Disabled", v1alpha1.NodeAutoscalerEnabledDisabled},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.expected, loadNodeAutoscalerEnabledConfig(t, testCase.value))
		})
	}
}
