package cluster_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// unmanagedNotice is the start of the line switch prints for a context KSail does not manage.
const unmanagedNotice = "is unmanaged: the kubeconfig does not show it"

// writeSwitchKubeconfig writes a kubeconfig whose contexts map context name to the
// server URL of the context's own cluster, and returns its path.
func writeSwitchKubeconfig(t *testing.T, servers map[string]string) string {
	t.Helper()

	config := clientcmdapi.NewConfig()

	for contextName, server := range servers {
		config.Clusters[contextName] = &clientcmdapi.Cluster{Server: server}
		config.AuthInfos[contextName] = &clientcmdapi.AuthInfo{}
		config.Contexts[contextName] = &clientcmdapi.Context{
			Cluster:  contextName,
			AuthInfo: contextName,
		}
	}

	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, clientcmd.WriteToFile(*config, path))

	return path
}

// capturePickerItems returns switch deps whose picker records the offered items and
// selects want.
func capturePickerItems(path, want string, items *[]string) cluster.SwitchDeps {
	return cluster.SwitchDeps{
		KubeconfigPath:      path,
		LoadSwitchHistory:   func() []string { return nil },
		SaveToSwitchHistory: func(string) {},
		PickCluster: func(_ string, offered []string) (string, error) {
			*items = offered

			for _, item := range offered {
				if item == want {
					return item, nil
				}
			}

			return "", fmt.Errorf("%q not offered: %w", want, cluster.ErrNoClusters)
		},
	}
}

func currentContext(t *testing.T, path string) string {
	t.Helper()

	config, err := clientcmd.LoadFromFile(path)
	require.NoError(t, err)

	return config.CurrentContext
}

func TestSwitchPicker_MixedKubeconfigListsUnmanagedAfterManaged(t *testing.T) {
	t.Parallel()

	path := writeSwitchKubeconfig(t, map[string]string{
		"kind-dev":     "https://127.0.0.1:6443",
		"k3d-staging":  "https://127.0.0.1:7443",
		"corp-prod":    "https://corp-prod.example.com",
		"aaa-external": "https://external.example.com",
	})

	var items []string

	cmd, buf := newSwitchTestCmd()
	deps := capturePickerItems(path, "corp-prod (unmanaged)", &items)

	picked, err := cluster.ExportPickCluster(cmd, deps)
	require.NoError(t, err)

	// Managed clusters keep their names and come first; unmanaged contexts follow,
	// sorted by context name and marked.
	assert.Equal(t, []string{
		"dev",
		"staging",
		"aaa-external (unmanaged)",
		"corp-prod (unmanaged)",
	}, items)

	require.NoError(t, cluster.HandleSwitchRunE(cmd, picked, deps))
	assert.Equal(t, "corp-prod", currentContext(t, path))
	assert.Contains(t, buf.String(), "Switched to cluster 'corp-prod' (context: corp-prod)")
	assert.Contains(t, buf.String(), "context 'corp-prod' "+unmanagedNotice)
}

func TestSwitchPicker_RecentManagedClustersStayFirst(t *testing.T) {
	t.Parallel()

	path := writeSwitchKubeconfig(t, map[string]string{
		"kind-alpha": "https://127.0.0.1:6443",
		"kind-beta":  "https://127.0.0.1:7443",
		"corp-prod":  "https://corp-prod.example.com",
	})

	var items []string

	cmd, _ := newSwitchTestCmd()
	deps := capturePickerItems(path, "beta", &items)
	deps.LoadSwitchHistory = func() []string { return []string{"beta", "corp-prod"} }

	_, err := cluster.ExportPickCluster(cmd, deps)
	require.NoError(t, err)

	assert.Equal(t, []string{"beta", "alpha", "corp-prod (unmanaged)"}, items)
}

func TestSwitchPicker_OnlyUnmanagedContextsAreOffered(t *testing.T) {
	t.Parallel()

	path := writeSwitchKubeconfig(t, map[string]string{
		"unknown-cluster": "https://127.0.0.1:6443",
	})

	var items []string

	cmd, buf := newSwitchTestCmd()
	deps := capturePickerItems(path, "unknown-cluster (unmanaged)", &items)

	picked, err := cluster.ExportPickCluster(cmd, deps)
	require.NoError(t, err)
	assert.Equal(t, []string{"unknown-cluster (unmanaged)"}, items)

	require.NoError(t, cluster.HandleSwitchRunE(cmd, picked, deps))
	assert.Equal(t, "unknown-cluster", currentContext(t, path))
	assert.Contains(t, buf.String(), unmanagedNotice)
}

func TestSwitchPicker_UnmanagedLabelSelectsThatExactContext(t *testing.T) {
	t.Parallel()

	// An unmanaged context named like a managed cluster: the marked label selects
	// the unmanaged context, while the plain name stays ambiguous as before.
	path := writeSwitchKubeconfig(t, map[string]string{
		"kind-dev": "https://127.0.0.1:6443",
		"dev":      "https://dev.example.com",
	})

	var items []string

	cmd, buf := newSwitchTestCmd()
	deps := capturePickerItems(path, "dev (unmanaged)", &items)

	_, err := cluster.ExportPickCluster(cmd, deps)
	require.NoError(t, err)
	assert.Equal(t, []string{"dev", "dev (unmanaged)"}, items)

	require.NoError(t, cluster.HandleSwitchRunE(cmd, "dev (unmanaged)", deps))
	assert.Equal(t, "dev", currentContext(t, path))
	assert.Contains(t, buf.String(), unmanagedNotice)

	err = cluster.HandleSwitchRunE(cmd, "dev", deps)
	require.ErrorIs(t, err, cluster.ErrAmbiguousCluster)
	assert.Equal(t, "dev", currentContext(t, path))
}

func TestSwitchPicker_ManagedContextsWithoutPrefixAreNotMarked(t *testing.T) {
	t.Parallel()

	// eksctl and Sidero Omni contexts carry no distribution prefix, but KSail
	// manages the clusters behind them, so they are listed by raw name, unmarked.
	path := writeSwitchKubeconfig(t, map[string]string{
		"deployer@shop.eu-west-1.eksctl.io": "https://shop.eks.example.com",
		"acme-shop-sa":                      "https://acme.kubernetes.eu-1.omni.siderolabs.io",
	})

	var items []string

	cmd, buf := newSwitchTestCmd()
	deps := capturePickerItems(path, "acme-shop-sa", &items)

	picked, err := cluster.ExportPickCluster(cmd, deps)
	require.NoError(t, err)
	assert.Equal(t, []string{"acme-shop-sa", "deployer@shop.eu-west-1.eksctl.io"}, items)

	require.NoError(t, cluster.HandleSwitchRunE(cmd, picked, deps))
	assert.Equal(t, "acme-shop-sa", currentContext(t, path))
	assert.NotContains(t, buf.String(), unmanagedNotice)
}

func TestSwitchCmd_TypedUnmanagedNameBeatsSubstringMatches(t *testing.T) {
	t.Parallel()

	// Typing an unmanaged context's exact name switches to it, even when other
	// contexts contain that name and would otherwise make it ambiguous.
	path := writeSwitchKubeconfig(t, map[string]string{
		"corp":    "https://corp.example.com",
		"corp-eu": "https://corp-eu.example.com",
	})

	cmd, buf := newSwitchTestCmd()

	require.NoError(t, cluster.HandleSwitchRunE(cmd, "corp", cluster.SwitchDeps{
		KubeconfigPath:      path,
		SaveToSwitchHistory: func(string) {},
	}))
	assert.Equal(t, "corp", currentContext(t, path))
	assert.Contains(t, buf.String(), unmanagedNotice)
}

func TestSwitchCompletion_ListsUnmanagedContexts(t *testing.T) {
	path := writeSwitchKubeconfig(t, map[string]string{
		"kind-dev":                          "https://127.0.0.1:6443",
		"dev":                               "https://dev.example.com",
		"corp-prod":                         "https://corp-prod.example.com",
		"deployer@shop.eu-west-1.eksctl.io": "https://shop.eks.example.com",
	})

	t.Setenv("KUBECONFIG", path)

	cmd, _ := newSwitchTestCmd()

	// The unmanaged "dev" context is left out: "dev" is already offered.
	assert.Equal(t, []string{
		"dev",
		"corp-prod\tunmanaged",
		"deployer@shop.eu-west-1.eksctl.io",
	}, cluster.ExportListClusterNames(cmd))
}

func TestSwitchCompletion_OnlyUnmanagedContexts(t *testing.T) {
	path := writeSwitchKubeconfig(t, map[string]string{
		"unknown-cluster": "https://127.0.0.1:6443",
	})

	t.Setenv("KUBECONFIG", path)

	cmd, _ := newSwitchTestCmd()

	assert.Equal(t, []string{"unknown-cluster\tunmanaged"}, cluster.ExportListClusterNames(cmd))
}

func TestSwitchCmd_StaleUnmanagedLabelDoesNotSwitchElsewhere(t *testing.T) {
	t.Parallel()

	// The labelled context is gone; a context that merely contains its name must not
	// be selected instead.
	path := writeSwitchKubeconfig(t, map[string]string{
		"kind-dev":     "https://127.0.0.1:6443",
		"corp-prod-eu": "https://corp-prod-eu.example.com",
	})

	cmd, _ := newSwitchTestCmd()

	err := cluster.HandleSwitchRunE(cmd, "corp-prod (unmanaged)", cluster.SwitchDeps{
		KubeconfigPath:      path,
		SaveToSwitchHistory: func(string) {},
	})
	require.ErrorIs(t, err, cluster.ErrContextNotFound)
	assert.Empty(t, currentContext(t, path))
}
