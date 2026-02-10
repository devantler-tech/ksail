package metricsserverinstaller_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	metricsserverinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/metricsserver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewInstaller(t *testing.T) {
	t.Parallel()

	kubeconfig := "~/.kube/config"
	kubeContext := "test-context"
	timeout := 5 * time.Minute

	client := helm.NewMockInterface(t)
	installer := metricsserverinstaller.NewInstaller(
		client,
		kubeconfig,
		kubeContext,
		timeout,
	)

	assert.NotNil(t, installer)
}

func TestMetricsServerInstallerInstallSuccess(t *testing.T) {
	t.Parallel()

	installer, client := newMetricsServerInstallerWithDefaults(t)
	expectMetricsServerInstall(t, client, nil)

	err := installer.Install(context.Background())

	require.NoError(t, err)
}

func TestMetricsServerInstallerInstallError(t *testing.T) {
	t.Parallel()

	installer, client := newMetricsServerInstallerWithDefaults(t)
	expectMetricsServerInstall(t, client, assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to install metrics-server")
}

func TestMetricsServerInstallerInstallAddRepositoryError(t *testing.T) {
	t.Parallel()

	installer, client := newMetricsServerInstallerWithDefaults(t)
	expectMetricsServerAddRepository(t, client, assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add metrics-server repository")
}

func newMetricsServerInstallerWithDefaults(
	t *testing.T,
) (*metricsserverinstaller.Installer, *helm.MockInterface) {
	t.Helper()

	kubeconfig := "~/.kube/config"
	kubeContext := "test-context"
	timeout := 5 * time.Second

	client := helm.NewMockInterface(t)
	installer := metricsserverinstaller.NewInstaller(
		client,
		kubeconfig,
		kubeContext,
		timeout,
	)

	return installer, client
}

func expectMetricsServerAddRepository(
	t *testing.T,
	client *helm.MockInterface,
	err error,
) {
	t.Helper()
	client.EXPECT().
		AddRepository(
			mock.Anything,
			mock.MatchedBy(func(entry *helm.RepositoryEntry) bool {
				assert.Equal(t, "metrics-server", entry.Name)
				assert.Equal(t, "https://kubernetes-sigs.github.io/metrics-server/", entry.URL)

				return true
			}),
			mock.Anything,
		).
		Return(err)
}

func expectMetricsServerInstall(
	t *testing.T,
	client *helm.MockInterface,
	installErr error,
) {
	t.Helper()
	expectMetricsServerAddRepository(t, client, nil)
	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				assert.Equal(t, "metrics-server", spec.ReleaseName)
				assert.Equal(t, "metrics-server/metrics-server", spec.ChartName)
				assert.Equal(t, "kube-system", spec.Namespace)
				assert.Equal(t, "https://kubernetes-sigs.github.io/metrics-server/", spec.RepoURL)
				assert.True(t, spec.Atomic)
				assert.True(t, spec.Wait)
				assert.True(t, spec.WaitForJobs)

				return true
			}),
		).
		Return(nil, installErr)
}
