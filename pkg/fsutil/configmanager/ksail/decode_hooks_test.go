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
