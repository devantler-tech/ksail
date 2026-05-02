package argocdinstaller_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	argocdinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/argocd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewInstaller(t *testing.T) {
	t.Parallel()

	timeout := 5 * time.Minute
	client := helm.NewMockInterface(t)
	installer := argocdinstaller.NewInstaller(client, timeout, false, false)

	require.NotNil(t, installer)
}

func TestNewInstaller_HAEnabled(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := argocdinstaller.NewInstaller(client, 5*time.Minute, false, true)

	require.NotNil(t, installer)
}

func TestChartSpec_HAEnabled(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	inst := argocdinstaller.NewInstaller(client, 5*time.Minute, false, true)
	spec := inst.ChartSpec()

	require.NotNil(t, spec.SetValues)
	assert.Equal(t, "2", spec.SetValues["server.replicas"])
	assert.Equal(t, "2", spec.SetValues["repoServer.replicas"])
	assert.Equal(t, "true", spec.SetValues["server.pdb.enabled"])
	assert.Equal(t, "1", spec.SetValues["server.pdb.minAvailable"])
	assert.Equal(t, "true", spec.SetValues["repoServer.pdb.enabled"])
	assert.Equal(t, "1", spec.SetValues["repoServer.pdb.minAvailable"])
}

func TestChartSpec_HADisabled(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	inst := argocdinstaller.NewInstaller(client, 5*time.Minute, false, false)
	spec := inst.ChartSpec()

	assert.Empty(t, spec.SetValues, "SetValues should be empty when HA is disabled")
}

func TestChartSpecValuesYaml(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		sopsEnabled bool
		expectYAML  bool
	}{
		{
			name:        "sops disabled",
			sopsEnabled: false,
			expectYAML:  false,
		},
		{
			name:        "sops enabled",
			sopsEnabled: true,
			expectYAML:  true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			client := helm.NewMockInterface(t)
			inst := argocdinstaller.NewInstaller(
				client, 5*time.Minute, testCase.sopsEnabled, false,
			)
			spec := inst.ChartSpec()

			if testCase.expectYAML {
				assert.NotEmpty(t, spec.ValuesYaml,
					"ValuesYaml should be set when SOPS is enabled")
				assert.Contains(t, spec.ValuesYaml, "kustomize-sops",
					"ValuesYaml should reference the CMP plugin")
			} else {
				assert.Empty(t, spec.ValuesYaml,
					"ValuesYaml should be empty when SOPS is disabled")
			}
		})
	}
}

func TestArgoCDInstallerInstallSuccess(t *testing.T) {
	t.Parallel()

	installer, client := newArgoCDInstallerWithDefaults(t)
	expectArgoCDInstall(t, client, nil)

	err := installer.Install(context.Background())
	require.NoError(t, err)
}

func TestArgoCDInstallerInstallError(t *testing.T) {
	t.Parallel()

	installer, client := newArgoCDInstallerWithDefaults(t)
	expectArgoCDInstall(t, client, assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to install Argo CD")
}

func TestArgoCDInstallerUninstallSuccess(t *testing.T) {
	t.Parallel()

	installer, client := newArgoCDInstallerWithDefaults(t)
	expectArgoCDUninstall(t, client, nil)

	err := installer.Uninstall(context.Background())

	require.NoError(t, err)
}

func TestArgoCDInstallerUninstallError(t *testing.T) {
	t.Parallel()

	installer, client := newArgoCDInstallerWithDefaults(t)
	expectArgoCDUninstall(t, client, assert.AnError)

	err := installer.Uninstall(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to uninstall Argo CD release")
}

func newArgoCDInstallerWithDefaults(
	t *testing.T,
) (*argocdinstaller.Installer, *helm.MockInterface) {
	t.Helper()
	client := helm.NewMockInterface(t)
	installer := argocdinstaller.NewInstaller(
		client,
		5*time.Second,
		false,
		false,
	)

	return installer, client
}

func expectArgoCDInstall(t *testing.T, client *helm.MockInterface, installErr error) {
	t.Helper()
	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				assert.Equal(t, "argocd", spec.ReleaseName)
				assert.Equal(t, "oci://ghcr.io/argoproj/argo-helm/argo-cd", spec.ChartName)
				assert.Equal(t, "argocd", spec.Namespace)
				assert.True(t, spec.CreateNamespace)
				assert.True(t, spec.Atomic)
				assert.True(t, spec.UpgradeCRDs)

				return true
			}),
		).
		Return(nil, installErr)
}

func expectArgoCDUninstall(t *testing.T, client *helm.MockInterface, err error) {
	t.Helper()
	client.EXPECT().
		UninstallRelease(mock.Anything, "argocd", "argocd").
		Return(err)
}
