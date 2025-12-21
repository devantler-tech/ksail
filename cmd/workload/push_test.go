package workload_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v5/cmd"
	"github.com/stretchr/testify/require"
)

func TestPushCmdValidateFlag(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	root := cmd.NewRootCmd("test", "test", "test")
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"workload", "push", "--help"})

	err := root.Execute()
	require.NoError(t, err)

	output := out.String()
	require.Contains(t, output, "--validate", "expected --validate flag in help output")
	require.Contains(
		t,
		output,
		"Validate workloads before pushing",
		"expected validation flag description",
	)
}

//nolint:paralleltest // Uses t.Chdir which is incompatible with parallel tests.
func TestPushCmdWithValidateFlagRequiresGitOpsEngine(t *testing.T) {
	var out bytes.Buffer

	tempDir := t.TempDir()
	writeValidKsailConfig(t, tempDir)

	t.Chdir(tempDir)

	root := cmd.NewRootCmd("test", "test", "test")
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"workload", "push", "--validate"})

	err := root.Execute()
	require.ErrorContains(
		t,
		err,
		"GitOps engine must be enabled",
		"expected push command to require GitOps engine even with --validate flag",
	)
}

//nolint:paralleltest // Uses t.Chdir which is incompatible with parallel tests.
func TestPushCmdWithValidateOnPushConfig(t *testing.T) {
	var out bytes.Buffer

	tempDir := t.TempDir()
	workloadDir := filepath.Join(tempDir, "k8s")
	require.NoError(t, os.MkdirAll(workloadDir, 0o750))

	// Create a valid Kubernetes manifest for validation
	manifestContent := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
  namespace: default
data:
  key: value
`
	manifestPath := filepath.Join(workloadDir, "configmap.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestContent), 0o600))

	// Create ksail config with validateOnPush enabled
	ksailConfigContent := `apiVersion: ksail.dev/v1alpha1
kind: Cluster
spec:
  distribution: Kind
  distributionConfig: kind.yaml
  workload:
    sourceDirectory: k8s
    validateOnPush: true
`
	configPath := filepath.Join(tempDir, "ksail.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(ksailConfigContent), 0o600))

	kindConfigContent := `kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: kind
`
	kindConfigPath := filepath.Join(tempDir, "kind.yaml")
	require.NoError(t, os.WriteFile(kindConfigPath, []byte(kindConfigContent), 0o600))

	t.Chdir(tempDir)

	root := cmd.NewRootCmd("test", "test", "test")
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"workload", "push"})

	err := root.Execute()
	// Should still require GitOps engine, but the test verifies the config is parsed
	require.ErrorContains(
		t,
		err,
		"GitOps engine must be enabled",
		"expected push command to require GitOps engine",
	)

	// Verify config was loaded (which includes validateOnPush field)
	output := out.String()
	require.Contains(t, output, "config loaded")
}
