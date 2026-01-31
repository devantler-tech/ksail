package cloudproviderkindinstaller_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	cloudproviderkindinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/cloudproviderkind"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewCloudProviderKINDInstaller(t *testing.T) {
	t.Parallel()

	kubeconfig := "~/.kube/config"
	kubeContext := "test-context"
	timeout := 5 * time.Minute

	client := helm.NewMockInterface(t)
	installer := cloudproviderkindinstaller.NewCloudProviderKINDInstaller(
		client,
		kubeconfig,
		kubeContext,
		timeout,
	)

	assert.NotNil(t, installer)
}

func TestCloudProviderKINDInstallerInstallSuccess(t *testing.T) {
	t.Parallel()

	installer, client := newCloudProviderKINDInstallerWithDefaults(t)
	expectCloudProviderKINDInstall(t, client, nil)

	err := installer.Install(context.Background())

	require.NoError(t, err)
}

func TestCloudProviderKINDInstallerInstallError(t *testing.T) {
	t.Parallel()

	installer, client := newCloudProviderKINDInstallerWithDefaults(t)
	expectCloudProviderKINDInstall(t, client, assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to install cloud-provider-kind")
}

func TestCloudProviderKINDInstallerInstallAddRepositoryError(t *testing.T) {
	t.Parallel()

	installer, client := newCloudProviderKINDInstallerWithDefaults(t)
	expectCloudProviderKINDAddRepository(t, client, assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add cloud-provider-kind repository")
}

func TestCloudProviderKINDInstallerUninstallSuccess(t *testing.T) {
	t.Parallel()

	installer, client := newCloudProviderKINDInstallerWithDefaults(t)
	client.EXPECT().
		UninstallRelease(mock.Anything, "cloud-provider-kind", "kube-system").
		Return(nil)

	err := installer.Uninstall(context.Background())

	require.NoError(t, err)
}

func TestCloudProviderKINDInstallerUninstallError(t *testing.T) {
	t.Parallel()

	installer, client := newCloudProviderKINDInstallerWithDefaults(t)
	client.EXPECT().
		UninstallRelease(mock.Anything, "cloud-provider-kind", "kube-system").
		Return(assert.AnError)

	err := installer.Uninstall(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to uninstall cloud-provider-kind")
}

func newCloudProviderKINDInstallerWithDefaults(
	t *testing.T,
) (*cloudproviderkindinstaller.CloudProviderKINDInstaller, *helm.MockInterface) {
	t.Helper()

	kubeconfig := "~/.kube/config"
	kubeContext := "test-context"
	timeout := 5 * time.Second

	client := helm.NewMockInterface(t)
	installer := cloudproviderkindinstaller.NewCloudProviderKINDInstaller(
		client,
		kubeconfig,
		kubeContext,
		timeout,
	)

	return installer, client
}

func expectCloudProviderKINDAddRepository(
	t *testing.T,
	client *helm.MockInterface,
	err error,
) {
	t.Helper()
	client.EXPECT().
		AddRepository(
			mock.Anything,
			mock.MatchedBy(func(entry *helm.RepositoryEntry) bool {
				assert.Equal(t, "cloud-provider-kind", entry.Name)
				assert.Equal(t, "https://kubernetes-sigs.github.io/cloud-provider-kind", entry.URL)

				return true
			}),
			mock.Anything,
		).
		Return(err)
}

func expectCloudProviderKINDInstall(
	t *testing.T,
	client *helm.MockInterface,
	installErr error,
) {
	t.Helper()
	expectCloudProviderKINDAddRepository(t, client, nil)
	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				assert.Equal(t, "cloud-provider-kind", spec.ReleaseName)
				assert.Equal(
					t,
					"cloud-provider-kind/cloud-provider-kind",
					spec.ChartName,
				)
				assert.Equal(t, "kube-system", spec.Namespace)
				assert.Equal(
					t,
					"https://kubernetes-sigs.github.io/cloud-provider-kind",
					spec.RepoURL,
				)
				assert.True(t, spec.Atomic)
				assert.True(t, spec.Wait)
				assert.True(t, spec.WaitForJobs)

				return true
			}),
		).
		Return(nil, installErr)
}
