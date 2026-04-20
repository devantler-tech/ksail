package calicoinstaller_test

import (
	"context"
	"testing"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	calicoinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/cni/calico"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestInstaller_Install_TalosDistribution(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := calicoinstaller.NewInstallerWithDistribution(
		client,
		"/nonexistent/kubeconfig",
		"test-context",
		2*time.Minute,
		v1alpha1.DistributionTalos,
	)

	installer.SetAPIServerCheckerForTest(func(_ context.Context) error { return nil })

	// Install will fail at ensurePrivilegedNamespaces because there is no real cluster.
	// This still covers the Talos-specific Install path (PrepareInstall + Talos branch).
	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "privileged namespaces")
}

func TestInstaller_Install_EmptyDistribution(t *testing.T) {
	t.Parallel()

	installer, client := newCovInstallerWithDistribution(t, "")
	expectCovCalicoInstall(t, client, nil)

	err := installer.Install(context.Background())

	require.NoError(t, err)
}

func TestInstaller_Install_VClusterDistribution(t *testing.T) {
	t.Parallel()

	installer, client := newCovInstallerWithDistribution(t, v1alpha1.DistributionVCluster)
	expectCovCalicoInstall(t, client, nil)

	err := installer.Install(context.Background())

	require.NoError(t, err)
}

func TestInstaller_Install_APIServerCheckerError(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := calicoinstaller.NewInstallerWithDistribution(
		client,
		"/path/to/kubeconfig",
		"test-context",
		2*time.Minute,
		v1alpha1.DistributionK3s,
	)

	installer.SetAPIServerCheckerForTest(func(_ context.Context) error {
		return assert.AnError
	})

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "API server stability")
}

func TestInstaller_Images_Success(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := calicoinstaller.NewInstallerWithDistribution(
		client,
		"/path/to/kubeconfig",
		"test-context",
		5*time.Minute,
		v1alpha1.DistributionVanilla,
	)

	manifest := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: calico-node
spec:
  template:
    spec:
      containers:
      - name: calico-node
        image: calico/node:v3.28.0
`

	client.EXPECT().
		TemplateChart(mock.Anything, mock.Anything).
		Return(manifest, nil)

	images, err := installer.Images(context.Background())

	require.NoError(t, err)
	assert.NotEmpty(t, images)
	assert.Contains(t, images, "docker.io/calico/node:v3.28.0")
}

func TestInstaller_Images_NilClient(t *testing.T) {
	t.Parallel()

	installer := calicoinstaller.NewInstallerWithDistribution(
		nil,
		"/path/to/kubeconfig",
		"test-context",
		5*time.Minute,
		v1alpha1.DistributionVanilla,
	)

	images, err := installer.Images(context.Background())

	require.Error(t, err)
	assert.Nil(t, images)
	assert.Contains(t, err.Error(), "helm client is nil")
}

func TestInstaller_Uninstall_ContextCanceled(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := calicoinstaller.NewInstallerWithDistribution(
		client,
		"/path/to/kubeconfig",
		"test-context",
		2*time.Minute,
		v1alpha1.DistributionVanilla,
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client.EXPECT().
		UninstallRelease(mock.Anything, "calico", "tigera-operator").
		Return(ctx.Err())

	err := installer.Uninstall(ctx)

	require.Error(t, err)
}

// --- test helpers ---

func newCovInstallerWithDistribution(
	t *testing.T,
	distribution v1alpha1.Distribution,
) (*calicoinstaller.Installer, *helm.MockInterface) {
	t.Helper()

	client := helm.NewMockInterface(t)
	installer := calicoinstaller.NewInstallerWithDistribution(
		client,
		"/path/to/kubeconfig",
		"test-context",
		2*time.Minute,
		distribution,
	)

	return installer, client
}

func expectCovCalicoInstall(t *testing.T, client *helm.MockInterface, installErr error) {
	t.Helper()

	client.EXPECT().
		AddRepository(
			mock.Anything,
			mock.MatchedBy(func(entry *helm.RepositoryEntry) bool {
				return entry != nil && entry.Name == "projectcalico"
			}),
			mock.Anything,
		).
		Return(nil)

	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				return spec != nil && spec.ReleaseName == "calico"
			}),
		).
		Return(nil, installErr)
}
