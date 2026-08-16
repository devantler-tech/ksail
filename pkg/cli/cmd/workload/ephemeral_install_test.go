package workload_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// writeEphemeralChartFixture writes a kustomization declaring one HelmRelease
// (podinfo 1.0.0 from an in-stream HelmRepository) into dir.
func writeEphemeralChartFixture(t *testing.T, dir string) {
	t.Helper()

	files := map[string]string{
		"kustomization.yaml": `resources:
  - helm-repository.yaml
  - helm-release.yaml
`,
		"helm-repository.yaml": `apiVersion: source.toolkit.fluxcd.io/v1
kind: HelmRepository
metadata:
  name: podinfo
  namespace: test
spec:
  url: https://example.com/charts
`,
		"helm-release.yaml": `apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: podinfo
  namespace: test
spec:
  interval: 1m
  chart:
    spec:
      chart: podinfo
      version: 1.0.0
      sourceRef:
        kind: HelmRepository
        name: podinfo
  values:
    replicaCount: 2
`,
	}

	for name, content := range files {
		err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600)
		require.NoError(t, err)
	}
}

// installEphemeralHelmClient seams a mock install client into
// installDeclaredCharts and records the connection it was built for.
func installEphemeralHelmClient(
	t *testing.T,
	client helm.Interface,
) (*string, *string) {
	t.Helper()

	var kubeconfigPath, kubeContext string

	restore := workload.ExportSetEphemeralHelmClient(
		func(gotKubeconfig, gotContext string) (helm.Interface, error) {
			kubeconfigPath = gotKubeconfig
			kubeContext = gotContext

			return client, nil
		},
	)
	t.Cleanup(restore)

	return &kubeconfigPath, &kubeContext
}

//nolint:paralleltest // swaps the shared newEphemeralHelmClient package var
func TestInstallDeclaredChartsInstallsEnumeratedCharts(t *testing.T) {
	dir := t.TempDir()
	writeEphemeralChartFixture(t, dir)

	client := helm.NewMockInterface(t)

	var installed []*helm.ChartSpec

	client.EXPECT().
		InstallOrUpgradeChart(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, spec *helm.ChartSpec) (*helm.ReleaseInfo, error) {
			installed = append(installed, spec)

			return &helm.ReleaseInfo{}, nil
		})

	gotKubeconfig, gotContext := installEphemeralHelmClient(t, client)

	cluster := workload.EphemeralCluster{
		Name:           "ksail-ephemeral-test",
		KubeconfigPath: "/tmp/kubeconfig",
		Context:        "kind-ksail-ephemeral-test",
	}

	err := workload.ExportInstallDeclaredCharts(
		t.Context(), newTestCommand(t), cluster, dir,
	)
	require.NoError(t, err)

	require.Len(t, installed, 1)
	assert.Equal(t, "podinfo", installed[0].ReleaseName)
	assert.Equal(t, "test", installed[0].Namespace)
	assert.Equal(t, "1.0.0", installed[0].Version)
	assert.True(t, installed[0].CreateNamespace)
	assert.Equal(t, "/tmp/kubeconfig", *gotKubeconfig)
	assert.Equal(t, "kind-ksail-ephemeral-test", *gotContext)
}

//nolint:paralleltest // swaps the shared newEphemeralHelmClient package var
func TestInstallDeclaredChartsSkipsClientWithoutCharts(t *testing.T) {
	restore := workload.ExportSetEphemeralHelmClient(
		func(string, string) (helm.Interface, error) {
			t.Fatal("helm client must not be constructed when no charts are declared")

			return nil, assert.AnError
		},
	)
	t.Cleanup(restore)

	err := workload.ExportInstallDeclaredCharts(
		t.Context(), newTestCommand(t), workload.EphemeralCluster{}, t.TempDir(),
	)
	require.NoError(t, err)
}

//nolint:paralleltest // swaps the shared newEphemeralHelmClient package var
func TestInstallDeclaredChartsFailsOnInstallError(t *testing.T) {
	dir := t.TempDir()
	writeEphemeralChartFixture(t, dir)

	client := helm.NewMockInterface(t)
	client.EXPECT().
		InstallOrUpgradeChart(mock.Anything, mock.Anything).
		Return(nil, assert.AnError)

	installEphemeralHelmClient(t, client)

	err := workload.ExportInstallDeclaredCharts(
		t.Context(), newTestCommand(t), workload.EphemeralCluster{Name: "eph"}, dir,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "install declared chart")
}

//nolint:paralleltest // swaps the shared newEphemeralHelmClient package var
func TestInstallDeclaredChartsDeduplicatesAcrossKustomizations(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "base")
	overlay := filepath.Join(root, "overlay")

	require.NoError(t, os.MkdirAll(base, 0o750))
	require.NoError(t, os.MkdirAll(overlay, 0o750))

	writeEphemeralChartFixture(t, base)

	err := os.WriteFile(
		filepath.Join(overlay, "kustomization.yaml"),
		[]byte("resources:\n  - ../base\n"),
		0o600,
	)
	require.NoError(t, err)

	client := helm.NewMockInterface(t)

	installs := 0

	client.EXPECT().
		InstallOrUpgradeChart(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, _ *helm.ChartSpec) (*helm.ReleaseInfo, error) {
			installs++

			return &helm.ReleaseInfo{}, nil
		})

	installEphemeralHelmClient(t, client)

	err = workload.ExportInstallDeclaredCharts(
		t.Context(), newTestCommand(t), workload.EphemeralCluster{Name: "eph"}, root,
	)
	require.NoError(t, err)
	assert.Equal(t, 1, installs)
}
