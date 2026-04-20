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

func TestEnsureDefaultResources_NilContext(t *testing.T) {
	t.Parallel()

	//nolint:staticcheck // Intentionally passing nil context to test the nil-context guard.
	err := argocdinstaller.EnsureDefaultResources(nil, "/fake/kubeconfig", 5*time.Second)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "context is nil")
}

func TestEnsureSopsAgeSecret_NilContext(t *testing.T) {
	t.Parallel()

	//nolint:staticcheck // Intentionally passing nil context to test the nil-context guard.
	err := argocdinstaller.EnsureSopsAgeSecret(nil, "/fake/kubeconfig", nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "context is nil")
}

func TestEnsureSopsAgeSecret_NilClusterCfg(t *testing.T) {
	t.Parallel()

	err := argocdinstaller.EnsureSopsAgeSecret(context.Background(), "/fake/kubeconfig", nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "clusterCfg is nil")
}

func TestArgoCDInstallerImages_Error(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := argocdinstaller.NewInstaller(client, 5*time.Minute, false)

	client.EXPECT().
		TemplateChart(mock.Anything, mock.Anything).
		Return("", assert.AnError)

	images, err := installer.Images(context.Background())

	require.Error(t, err)
	assert.Nil(t, images)
	assert.Contains(t, err.Error(), "listing images")
}

func TestArgoCDInstallerImages_Success(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := argocdinstaller.NewInstaller(client, 5*time.Minute, false)

	manifest := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: argocd-server
spec:
  template:
    spec:
      containers:
      - name: server
        image: quay.io/argoproj/argocd:v2.10.0
`

	client.EXPECT().
		TemplateChart(mock.Anything, mock.Anything).
		Return(manifest, nil)

	images, err := installer.Images(context.Background())

	require.NoError(t, err)
	assert.NotEmpty(t, images)
	assert.Contains(t, images, "quay.io/argoproj/argocd:v2.10.0")
}

func TestNewInstaller_SOPSEnabled(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := argocdinstaller.NewInstaller(client, 5*time.Minute, true)

	require.NotNil(t, installer)

	spec := installer.ChartSpec()
	assert.NotEmpty(t, spec.ValuesYaml, "ValuesYaml should be set when SOPS is enabled")
}

func TestChartSpec_SOPSDisabled_NoValuesYaml(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := argocdinstaller.NewInstaller(client, 5*time.Minute, false)

	spec := installer.ChartSpec()

	assert.Equal(t, "argocd", spec.ReleaseName)
	assert.Equal(t, "oci://ghcr.io/argoproj/argo-helm/argo-cd", spec.ChartName)
	assert.Equal(t, "argocd", spec.Namespace)
	assert.True(t, spec.CreateNamespace)
	assert.True(t, spec.Atomic)
	assert.True(t, spec.UpgradeCRDs)
	assert.Empty(t, spec.ValuesYaml, "ValuesYaml should be empty when SOPS is disabled")
}

func TestChartSpec_VersionNonEmpty(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := argocdinstaller.NewInstaller(client, 5*time.Minute, false)

	spec := installer.ChartSpec()

	assert.NotEmpty(t, spec.Version, "chart version should be non-empty")
}

func TestInstaller_Install_ContextCanceled(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := argocdinstaller.NewInstaller(client, 5*time.Second, false)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client.EXPECT().
		InstallOrUpgradeChart(mock.Anything, mock.Anything).
		Return(nil, ctx.Err())

	err := installer.Install(ctx)

	require.Error(t, err)
}
