package cmd_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	pkgcmd "github.com/devantler-tech/ksail/v5/pkg/cmd"
	"github.com/stretchr/testify/require"
)

func writeValidKsailConfig(t *testing.T, dir string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "k8s"), 0o750))

	ksailConfigContent := "apiVersion: ksail.dev/v1alpha1\n" +
		"kind: Cluster\n" +
		"spec:\n" +
		"  distribution: Kind\n" +
		"  distributionConfig: kind.yaml\n" +
		"  sourceDirectory: k8s\n"

	require.NoError(
		t,
		os.WriteFile(filepath.Join(dir, "ksail.yaml"), []byte(ksailConfigContent), 0o600),
	)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(dir, "kind.yaml"),
			[]byte("kind: Cluster\napiVersion: kind.x-k8s.io/v1alpha4\nname: kind\n"),
			0o600,
		),
	)
}

func TestGetDefaultKubeconfigPath(t *testing.T) {
	t.Parallel()

	path := pkgcmd.GetDefaultKubeconfigPath()

	homeDir, _ := os.UserHomeDir()
	expected := filepath.Join(homeDir, ".kube", "config")

	require.Equal(t, expected, path, "expected default kubeconfig path")
}

func TestGetKubeconfigPathFromConfig(t *testing.T) {
	t.Parallel()

	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	tests := []struct {
		name           string
		kubeconfigPath string
		expectedPath   string
	}{
		{
			name:           "empty path returns default",
			kubeconfigPath: "",
			expectedPath:   filepath.Join(homeDir, ".kube", "config"),
		},
		{
			name:           "absolute path unchanged",
			kubeconfigPath: "/tmp/kubeconfig",
			expectedPath:   "/tmp/kubeconfig",
		},
		{
			name:           "tilde path expanded",
			kubeconfigPath: "~/.kube/config",
			expectedPath:   filepath.Join(homeDir, ".kube", "config"),
		},
		{
			name:           "tilde path with nested dir expanded",
			kubeconfigPath: "~/custom/kubeconfig",
			expectedPath:   filepath.Join(homeDir, "custom", "kubeconfig"),
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cfg := &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Connection: v1alpha1.Connection{
						Kubeconfig: testCase.kubeconfigPath,
					},
				},
			}

			path, err := pkgcmd.GetKubeconfigPathFromConfig(cfg)
			require.NoError(t, err)
			require.Equal(t, testCase.expectedPath, path)
		})
	}
}

func TestGetKubeconfigPathSilently(t *testing.T) {
	t.Parallel()

	path := pkgcmd.GetKubeconfigPathSilently()

	require.NotEmpty(t, path, "expected non-empty kubeconfig path")
	require.True(
		t,
		filepath.IsAbs(path),
		"expected absolute path, got %s",
		path,
	)
}

//nolint:paralleltest // Uses t.Chdir which is incompatible with parallel tests.
func TestGetKubeconfigPathSilentlyWithValidConfig(t *testing.T) {
	tempDir := t.TempDir()
	writeValidKsailConfig(t, tempDir)
	t.Chdir(tempDir)

	path := pkgcmd.GetKubeconfigPathSilently()

	require.True(t, filepath.IsAbs(path), "expected absolute path")
}

//nolint:paralleltest // Uses t.Chdir which is incompatible with parallel tests.
func TestGetKubeconfigPathSilentlyWithMissingConfig(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	path := pkgcmd.GetKubeconfigPathSilently()
	require.True(t, filepath.IsAbs(path), "expected absolute path")
}
