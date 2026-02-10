package kubeconfig_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/kubeconfig"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetKubeconfigPathFromConfig_SpecifiedPath(t *testing.T) {
	t.Parallel()

	cfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Connection: v1alpha1.Connection{
					Kubeconfig: "/custom/kubeconfig",
				},
			},
		},
	}

	got, err := kubeconfig.GetKubeconfigPathFromConfig(cfg)
	require.NoError(t, err)
	assert.Equal(t, "/custom/kubeconfig", got)
}

func TestGetKubeconfigPathFromConfig_TildeExpansion(t *testing.T) {
	t.Parallel()

	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	cfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Connection: v1alpha1.Connection{
					Kubeconfig: "~/.kube/my-config",
				},
			},
		},
	}

	got, err := kubeconfig.GetKubeconfigPathFromConfig(cfg)
	require.NoError(t, err)
	assert.NotContains(t, got, "~")
	assert.True(t, filepath.IsAbs(got))

	snapshotPath := "HOME" + got[len(homeDir):]
	snaps.MatchSnapshot(t, snapshotPath)
}

func TestGetKubeconfigPathFromConfig_EmptyReturnsDefault(t *testing.T) {
	t.Parallel()

	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	cfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Connection: v1alpha1.Connection{
					Kubeconfig: "",
				},
			},
		},
	}

	got, err := kubeconfig.GetKubeconfigPathFromConfig(cfg)
	require.NoError(t, err)
	assert.NotEmpty(t, got)
	assert.True(t, filepath.IsAbs(got))

	snapshotPath := "HOME" + got[len(homeDir):]
	snaps.MatchSnapshot(t, snapshotPath)
}

func TestGetKubeconfigPathSilently(t *testing.T) {
	t.Parallel()

	// GetKubeconfigPathSilently should not panic and should return a valid path
	// even when there's no ksail.yaml config file
	path := kubeconfig.GetKubeconfigPathSilently()

	// Should return the default kubeconfig path when no config is found
	assert.NotEmpty(t, path)
	assert.True(t, filepath.IsAbs(path), "path should be absolute")
	assert.Contains(t, path, ".kube")
}
