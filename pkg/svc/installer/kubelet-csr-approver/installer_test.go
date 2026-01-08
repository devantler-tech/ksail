package kubeletcsrapproverinstaller_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	kubeletcsrapproverinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/kubelet-csr-approver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewKubeletCSRApproverInstaller(t *testing.T) {
	t.Parallel()

	timeout := 5 * time.Minute
	client := helm.NewMockInterface(t)
	installer := kubeletcsrapproverinstaller.NewKubeletCSRApproverInstaller(client, timeout)

	assert.NotNil(t, installer)
}

func TestKubeletCSRApproverInstallerInstallSuccess(t *testing.T) {
	t.Parallel()

	installer, client := newKubeletCSRApproverInstallerWithDefaults(t)
	expectKubeletCSRApproverInstall(t, client, nil)

	err := installer.Install(context.Background())

	require.NoError(t, err)
}

func TestKubeletCSRApproverInstallerInstallError(t *testing.T) {
	t.Parallel()

	installer, client := newKubeletCSRApproverInstallerWithDefaults(t)
	expectKubeletCSRApproverInstall(t, client, assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to install kubelet-csr-approver")
}

func TestKubeletCSRApproverInstallerInstallAddRepositoryError(t *testing.T) {
	t.Parallel()

	installer, client := newKubeletCSRApproverInstallerWithDefaults(t)
	expectKubeletCSRApproverAddRepository(t, client, assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add kubelet-csr-approver repository")
}

func TestKubeletCSRApproverInstallerUninstallSuccess(t *testing.T) {
	t.Parallel()

	installer, client := newKubeletCSRApproverInstallerWithDefaults(t)
	client.EXPECT().
		UninstallRelease(mock.Anything, "kubelet-csr-approver", "kube-system").
		Return(nil)

	err := installer.Uninstall(context.Background())

	require.NoError(t, err)
}

func TestKubeletCSRApproverInstallerUninstallError(t *testing.T) {
	t.Parallel()

	installer, client := newKubeletCSRApproverInstallerWithDefaults(t)
	client.EXPECT().
		UninstallRelease(mock.Anything, "kubelet-csr-approver", "kube-system").
		Return(assert.AnError)

	err := installer.Uninstall(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to uninstall kubelet-csr-approver")
}

func newKubeletCSRApproverInstallerWithDefaults(
	t *testing.T,
) (*kubeletcsrapproverinstaller.KubeletCSRApproverInstaller, *helm.MockInterface) {
	t.Helper()

	timeout := 5 * time.Second
	client := helm.NewMockInterface(t)
	installer := kubeletcsrapproverinstaller.NewKubeletCSRApproverInstaller(client, timeout)

	return installer, client
}

func expectKubeletCSRApproverAddRepository(
	t *testing.T,
	client *helm.MockInterface,
	err error,
) {
	t.Helper()
	client.EXPECT().
		AddRepository(
			mock.Anything,
			mock.MatchedBy(func(entry *helm.RepositoryEntry) bool {
				assert.Equal(t, "kubelet-csr-approver", entry.Name)
				assert.Equal(t, "https://postfinance.github.io/kubelet-csr-approver", entry.URL)

				return true
			}),
			mock.Anything,
		).
		Return(err)
}

func expectKubeletCSRApproverInstall(
	t *testing.T,
	client *helm.MockInterface,
	installErr error,
) {
	t.Helper()
	expectKubeletCSRApproverAddRepository(t, client, nil)
	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				assert.Equal(t, "kubelet-csr-approver", spec.ReleaseName)
				assert.Equal(t, "kubelet-csr-approver/kubelet-csr-approver", spec.ChartName)
				assert.Equal(t, "kube-system", spec.Namespace)
				assert.Equal(t, "https://postfinance.github.io/kubelet-csr-approver", spec.RepoURL)
				assert.True(t, spec.Atomic)
				assert.True(t, spec.Wait)
				assert.True(t, spec.WaitForJobs)
				assert.Contains(t, spec.ValuesYaml, "providerRegex")
				assert.Contains(t, spec.ValuesYaml, "bypassDnsResolution")

				return true
			}),
		).
		Return(nil, installErr)
}
