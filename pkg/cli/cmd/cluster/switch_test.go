package cluster_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
)

const testKubeconfigTwoContexts = `apiVersion: v1
kind: Config
current-context: kind-dev
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: kind-dev
- cluster:
    server: https://127.0.0.1:7443
  name: kind-staging
contexts:
- context:
    cluster: kind-dev
    user: kind-dev
  name: kind-dev
- context:
    cluster: kind-staging
    user: kind-staging
  name: kind-staging
users:
- name: kind-dev
  user: {}
- name: kind-staging
  user: {}
`

const testKubeconfigMultiDistro = `apiVersion: v1
kind: Config
current-context: kind-myapp
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: kind-myapp
- cluster:
    server: https://127.0.0.1:7443
  name: k3d-myapp
contexts:
- context:
    cluster: kind-myapp
    user: kind-myapp
  name: kind-myapp
- context:
    cluster: k3d-myapp
    user: k3d-myapp
  name: k3d-myapp
users:
- name: kind-myapp
  user: {}
- name: k3d-myapp
  user: {}
`

func newSwitchTestCmd() (*cobra.Command, *bytes.Buffer) {
	cmd := &cobra.Command{Use: "switch"}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())

	return cmd, &buf
}

func TestSwitchCmd_HappyPath(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	require.NoError(t, os.WriteFile(
		kubeconfigPath,
		[]byte(testKubeconfigTwoContexts),
		0o600,
	))

	cmd, buf := newSwitchTestCmd()
	deps := cluster.SwitchDeps{KubeconfigPath: kubeconfigPath}

	err := cluster.HandleSwitchRunE(cmd, "staging", deps)
	require.NoError(t, err)

	assert.Contains(t, buf.String(),
		"Switched to cluster 'staging' (context: kind-staging)")

	//nolint:gosec // G304: test-controlled path from t.TempDir()
	updatedBytes, err := os.ReadFile(kubeconfigPath)
	require.NoError(t, err)

	config, err := clientcmd.Load(updatedBytes)
	require.NoError(t, err)

	assert.Equal(t, "kind-staging", config.CurrentContext)
}

func TestSwitchCmd_K3sDistribution(t *testing.T) {
	t.Parallel()

	kubeconfig := `apiVersion: v1
kind: Config
current-context: ""
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: k3d-prod
contexts:
- context:
    cluster: k3d-prod
    user: k3d-prod
  name: k3d-prod
users:
- name: k3d-prod
  user: {}
`

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	require.NoError(t, os.WriteFile(
		kubeconfigPath, []byte(kubeconfig), 0o600,
	))

	cmd, buf := newSwitchTestCmd()
	deps := cluster.SwitchDeps{KubeconfigPath: kubeconfigPath}

	err := cluster.HandleSwitchRunE(cmd, "prod", deps)
	require.NoError(t, err)

	assert.Contains(t, buf.String(),
		"context: k3d-prod")

	//nolint:gosec // G304: test-controlled path from t.TempDir()
	updatedBytes, err := os.ReadFile(kubeconfigPath)
	require.NoError(t, err)

	config, err := clientcmd.Load(updatedBytes)
	require.NoError(t, err)

	assert.Equal(t, "k3d-prod", config.CurrentContext)
}

func TestSwitchCmd_TalosDistribution(t *testing.T) {
	t.Parallel()

	kubeconfig := `apiVersion: v1
kind: Config
current-context: ""
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: talos-cluster
contexts:
- context:
    cluster: talos-cluster
    user: admin@talos-cluster
  name: admin@talos-cluster
users:
- name: admin@talos-cluster
  user: {}
`

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	require.NoError(t, os.WriteFile(
		kubeconfigPath, []byte(kubeconfig), 0o600,
	))

	cmd, buf := newSwitchTestCmd()
	deps := cluster.SwitchDeps{KubeconfigPath: kubeconfigPath}

	err := cluster.HandleSwitchRunE(cmd, "talos-cluster", deps)
	require.NoError(t, err)

	assert.Contains(t, buf.String(),
		"context: admin@talos-cluster")

	//nolint:gosec // G304: test-controlled path from t.TempDir()
	updatedBytes, err := os.ReadFile(kubeconfigPath)
	require.NoError(t, err)

	config, err := clientcmd.Load(updatedBytes)
	require.NoError(t, err)

	assert.Equal(t, "admin@talos-cluster", config.CurrentContext)
}

func TestSwitchCmd_ContextNotFound(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	require.NoError(t, os.WriteFile(
		kubeconfigPath,
		[]byte(testKubeconfigTwoContexts),
		0o600,
	))

	cmd, _ := newSwitchTestCmd()
	deps := cluster.SwitchDeps{KubeconfigPath: kubeconfigPath}

	err := cluster.HandleSwitchRunE(cmd, "nonexistent", deps)
	require.Error(t, err)
	require.ErrorIs(t, err, cluster.ErrContextNotFound)
	assert.Contains(t, err.Error(), "nonexistent")
	assert.Contains(t, err.Error(), "available contexts")
}

func TestSwitchCmd_AmbiguousCluster(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	require.NoError(t, os.WriteFile(
		kubeconfigPath,
		[]byte(testKubeconfigMultiDistro),
		0o600,
	))

	cmd, _ := newSwitchTestCmd()
	deps := cluster.SwitchDeps{KubeconfigPath: kubeconfigPath}

	err := cluster.HandleSwitchRunE(cmd, "myapp", deps)
	require.Error(t, err)
	require.ErrorIs(t, err, cluster.ErrAmbiguousCluster)
	assert.Contains(t, err.Error(), "k3d-myapp")
	assert.Contains(t, err.Error(), "kind-myapp")
}

func TestSwitchCmd_KubeconfigNotFound(t *testing.T) {
	t.Parallel()

	cmd, _ := newSwitchTestCmd()

	deps := cluster.SwitchDeps{
		KubeconfigPath: "/nonexistent/path/kubeconfig",
	}

	err := cluster.HandleSwitchRunE(cmd, "some-cluster", deps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read kubeconfig")
}

func TestSwitchCmd_SameContext(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	require.NoError(t, os.WriteFile(
		kubeconfigPath,
		[]byte(testKubeconfigTwoContexts),
		0o600,
	))

	cmd, buf := newSwitchTestCmd()
	deps := cluster.SwitchDeps{KubeconfigPath: kubeconfigPath}

	err := cluster.HandleSwitchRunE(cmd, "dev", deps)
	require.NoError(t, err)

	assert.Contains(t, buf.String(),
		"Switched to cluster 'dev'")
}

func TestSwitchCmd_InteractivePicker(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	require.NoError(t, os.WriteFile(
		kubeconfigPath,
		[]byte(testKubeconfigTwoContexts),
		0o600,
	))

	cmd, buf := newSwitchTestCmd()

	deps := cluster.SwitchDeps{
		KubeconfigPath: kubeconfigPath,
		PickCluster: func(_ string, items []string) (string, error) {
			// Simulate user selecting "staging"
			for _, item := range items {
				if item == "staging" {
					return item, nil
				}
			}

			return "", fmt.Errorf("staging not in list: %w", cluster.ErrNoClusters)
		},
	}

	clusterName, err := cluster.ExportPickCluster(cmd, deps)
	require.NoError(t, err)
	assert.Equal(t, "staging", clusterName)

	err = cluster.HandleSwitchRunE(cmd, clusterName, deps)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Switched to cluster 'staging'")
}

func TestSwitchCmd_InteractivePicker_NoClusters(t *testing.T) {
	t.Parallel()

	// Kubeconfig with no KSail-managed contexts (no known distribution prefix)
	kubeconfig := `apiVersion: v1
kind: Config
current-context: ""
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: unknown-cluster
contexts:
- context:
    cluster: unknown-cluster
    user: some-user
  name: unknown-cluster
users:
- name: some-user
  user: {}
`

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	require.NoError(t, os.WriteFile(
		kubeconfigPath, []byte(kubeconfig), 0o600,
	))

	cmd, _ := newSwitchTestCmd()

	deps := cluster.SwitchDeps{
		KubeconfigPath: kubeconfigPath,
	}

	_, err := cluster.ExportPickCluster(cmd, deps)
	require.Error(t, err)
	require.ErrorIs(t, err, cluster.ErrNoClusters)
}

// TestSwitchCmd_InteractivePicker_NonTTY verifies the picker fails fast with an
// actionable error when no picker is injected and stdin is not a terminal (the
// go-test environment). This keeps the bubbletea picker from hanging or crashing
// for AI tool clients and CI; clusters exist, so the guard is what trips.
func TestSwitchCmd_InteractivePicker_NonTTY(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	require.NoError(t, os.WriteFile(
		kubeconfigPath, []byte(testKubeconfigTwoContexts), 0o600,
	))

	cmd, _ := newSwitchTestCmd()

	// No deps.PickCluster injected → default picker path → non-TTY guard.
	// IsTTY is injected false because the go-test stdin may report as a TTY.
	deps := cluster.SwitchDeps{
		KubeconfigPath: kubeconfigPath,
		IsTTY:          func() bool { return false },
	}

	_, err := cluster.ExportPickCluster(cmd, deps)
	require.Error(t, err)
	require.ErrorIs(t, err, cluster.ErrInteractivePickerNoTTY)
}

func TestSwitchCmd_FallbackKubeconfigFromEnv(t *testing.T) {
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	require.NoError(t, os.WriteFile(
		kubeconfigPath,
		[]byte(testKubeconfigTwoContexts),
		0o600,
	))

	t.Setenv("KUBECONFIG", kubeconfigPath)

	cmd, buf := newSwitchTestCmd()
	deps := cluster.SwitchDeps{}

	err := cluster.HandleSwitchRunE(cmd, "staging", deps)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Switched to cluster 'staging'")
}

func TestSwitchCmd_FallbackKubeconfigFromEnv_PickCluster(t *testing.T) {
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	require.NoError(t, os.WriteFile(
		kubeconfigPath,
		[]byte(testKubeconfigTwoContexts),
		0o600,
	))

	t.Setenv("KUBECONFIG", kubeconfigPath)

	cmd, _ := newSwitchTestCmd()
	deps := cluster.SwitchDeps{
		PickCluster: func(_ string, items []string) (string, error) {
			for _, item := range items {
				if item == "staging" {
					return item, nil
				}
			}

			return "", fmt.Errorf("staging not in list: %w", cluster.ErrNoClusters)
		},
	}

	clusterName, err := cluster.ExportPickCluster(cmd, deps)
	require.NoError(t, err)
	assert.Equal(t, "staging", clusterName)
}

func TestSwitchCmd_FallbackKubeconfigInvalid(t *testing.T) {
	t.Setenv("KUBECONFIG", "/nonexistent/invalid/kubeconfig")

	cmd, _ := newSwitchTestCmd()
	deps := cluster.SwitchDeps{}

	err := cluster.HandleSwitchRunE(cmd, "staging", deps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read kubeconfig")
}

func TestSwitchHistory_HandleSwitchRunE_UpdatesHistory(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	var saved []string

	loadFn := func() []string { return saved }
	saveFn := func(name string) {
		seen := map[string]struct{}{name: {}}
		updated := []string{name}

		for _, n := range saved {
			if _, dup := seen[n]; !dup {
				seen[n] = struct{}{}
				updated = append(updated, n)
			}
		}

		saved = updated
	}

	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfigPath, []byte(testKubeconfigTwoContexts), 0o600))

	cmd, _ := newSwitchTestCmd()

	// Switch to "dev" then "staging"
	deps := cluster.SwitchDeps{
		KubeconfigPath:      kubeconfigPath,
		LoadSwitchHistory:   loadFn,
		SaveToSwitchHistory: saveFn,
	}

	require.NoError(t, cluster.HandleSwitchRunE(cmd, "dev", deps))
	require.NoError(t, cluster.HandleSwitchRunE(cmd, "staging", deps))

	// "staging" was switched to last, so it should be first.
	require.Len(t, saved, 2)
	assert.Equal(t, "staging", saved[0])
	assert.Equal(t, "dev", saved[1])
}

// errPickerEmptyItems is a sentinel returned by test picker stubs when the
// items list is empty, so regressions produce a clean failure rather than a panic.
var errPickerEmptyItems = errors.New("picker received empty items list")

func TestSwitchHistory_PickerOrdersRecentFirst(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfigPath, []byte(testKubeconfigTwoContexts), 0o600))

	cmd, _ := newSwitchTestCmd()

	var pickerItems []string

	deps := cluster.SwitchDeps{
		KubeconfigPath:    kubeconfigPath,
		LoadSwitchHistory: func() []string { return []string{"staging"} },
		PickCluster: func(_ string, items []string) (string, error) {
			pickerItems = items
			if len(items) == 0 {
				return "", errPickerEmptyItems
			}

			return items[0], nil
		},
	}

	_, err := cluster.ExportPickCluster(cmd, deps)
	require.NoError(t, err)

	// "staging" was recently used, so it must appear first.
	require.NotEmpty(t, pickerItems)
	assert.Equal(t, "staging", pickerItems[0])
}

func TestSwitchHistory_RecentNotInKubeconfig(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfigPath, []byte(testKubeconfigTwoContexts), 0o600))

	cmd, _ := newSwitchTestCmd()

	// History references a cluster that no longer exists in the kubeconfig.
	var pickerItems []string

	deps := cluster.SwitchDeps{
		KubeconfigPath:    kubeconfigPath,
		LoadSwitchHistory: func() []string { return []string{"deleted-cluster"} },
		PickCluster: func(_ string, items []string) (string, error) {
			pickerItems = items
			if len(items) == 0 {
				return "", errPickerEmptyItems
			}

			return items[0], nil
		},
	}

	_, err := cluster.ExportPickCluster(cmd, deps)
	require.NoError(t, err)

	// "deleted-cluster" must not appear in the picker.
	for _, item := range pickerItems {
		assert.NotEqual(t, "deleted-cluster", item)
	}
}

func TestStripDistributionPrefix_UnknownReturnsEmpty(t *testing.T) {
	t.Parallel()
	assert.Empty(t, cluster.ExportStripDistributionPrefix("unknown-context"))
}

func TestStripDistributionPrefix_EmptyReturnsEmpty(t *testing.T) {
	t.Parallel()
	assert.Empty(t, cluster.ExportStripDistributionPrefix(""))
}

func TestStripDistributionPrefix_K3kReturnsClusterName(t *testing.T) {
	t.Parallel()
	// Nested K3s clusters (k3k operator on the Kubernetes provider) use a "k3k-"
	// context prefix rather than the standalone "k3d-"; they must still be stripped
	// to the bare cluster name so they appear in completion and the picker.
	assert.Equal(t, "nested", cluster.ExportStripDistributionPrefix("k3k-nested"))
}

func TestResolveContextName_K3kNestedK3sResolvesDeterministically(t *testing.T) {
	t.Parallel()

	// Nested K3s clusters (k3k operator on the Kubernetes provider) use a "k3k-"
	// context prefix rather than the standalone "k3d-". The explicit k3k- candidate
	// must win so resolution stays deterministic instead of falling back to substring
	// matching — which would also match the unrelated context below and report the
	// name as ambiguous.
	got, err := cluster.ExportResolveContextName(
		[]string{"k3k-nested", "external-nested-ctx"},
		"nested",
	)

	require.NoError(t, err)
	assert.Equal(t, "k3k-nested", got)
}
