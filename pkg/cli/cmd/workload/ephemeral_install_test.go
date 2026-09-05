package workload_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/ephemeral"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	assert.True(t, installed[0].Wait)
	assert.True(t, installed[0].WaitForJobs)
	assert.Equal(t, helm.DefaultTimeout, installed[0].Timeout)
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
func TestInstallDeclaredChartsUsesSelectedKustomization(t *testing.T) {
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
		t.Context(), newTestCommand(t), workload.EphemeralCluster{Name: "eph"}, overlay,
	)
	require.NoError(t, err)
	assert.Equal(t, 1, installs)
}

//nolint:paralleltest // replaces the ephemeral backend factory
func TestEphemeralOfflineFailureDoesNotCreateCluster(t *testing.T) {
	fake := &fakeEphemeralProvisioner{}
	installEphemeralProvisioner(t, fake, nil)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "kustomization.yaml"),
		[]byte("resources:\n  - absent.yaml\n"), 0o600))

	cmd := workload.NewValidateCmd()
	cmd.SetOut(os.Stdout)
	cmd.SetArgs([]string{dir, "--ephemeral"})
	err := cmd.ExecuteContext(t.Context())
	require.Error(t, err)
	assert.Empty(t, fake.created, "offline-invalid workloads must fail before provisioning")
}

//nolint:paralleltest // replaces the ephemeral backend factory
func TestEphemeralRejectsAmbiguousRootsBeforeProvisioning(t *testing.T) {
	fake := &fakeEphemeralProvisioner{}
	installEphemeralProvisioner(t, fake, nil)

	dir := t.TempDir()
	for _, name := range []string{"base", "overlay"} {
		root := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(root, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(root, "kustomization.yaml"),
			[]byte("resources: []\n"), 0o600))
	}

	cmd := workload.NewValidateCmd()
	cmd.SetArgs([]string{dir, "--ephemeral"})
	err := cmd.ExecuteContext(t.Context())
	require.ErrorContains(t, err, "select a Kustomize root")
	assert.Empty(t, fake.created)
}

type admissionRecorder struct {
	apply func(context.Context, *unstructured.Unstructured) error
}

func (r admissionRecorder) Apply(ctx context.Context, obj *unstructured.Unstructured) error {
	return r.apply(ctx, obj)
}

func (r admissionRecorder) WaitForCRD(context.Context, string) error { return nil }

func recordAdmission(
	t *testing.T,
	outcome string,
	cancel context.CancelFunc,
	requests *int,
) admissionRecorder {
	t.Helper()

	return admissionRecorder{
		apply: func(ctx context.Context, obj *unstructured.Unstructured) error {
			(*requests)++

			assert.Equal(t, "settings", obj.GetName())

			deadline, ok := ctx.Deadline()
			assert.True(t, ok)
			assert.LessOrEqual(t, time.Until(deadline), 10*time.Minute)

			if outcome == "rejected" {
				return assert.AnError
			}

			if outcome == "cancelled" {
				cancel()

				return fmt.Errorf("admission cancelled: %w", ctx.Err())
			}

			return nil
		},
	}
}

func admissionWorkload(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(root, "config.yaml"),
			[]byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: settings\n"),
			0o600,
		),
	)

	return root
}

//nolint:paralleltest // replaces the ephemeral lifecycle and client factories
func TestValidateAdmissionIsOptInAndAlwaysCleansUp(t *testing.T) {
	for _, outcome := range []string{"offline", "accepted", "rejected", "cancelled"} {
		t.Run(outcome, func(t *testing.T) {
			fake := &fakeEphemeralProvisioner{}
			backend := installEphemeralProvisioner(t, fake, nil)

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			requests := 0
			restore := workload.ExportSetEphemeralAdmissionClient(
				func(path, kubeContext string) (ephemeral.Client, error) {
					assert.Equal(t, filepath.Join(backend.workspace, "kubeconfig"), path)
					require.Len(t, fake.created, 1)
					assert.Equal(t, "kind-"+fake.created[0], kubeContext)

					return recordAdmission(t, outcome, cancel, &requests), nil
				},
			)
			t.Cleanup(restore)
			root := admissionWorkload(t)

			args := []string{root, "--skip-kinds", "ConfigMap"}
			if outcome != "offline" {
				args = append(args, "--ephemeral")
			}

			cmd := workload.NewValidateCmd()

			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetArgs(args)
			err := cmd.ExecuteContext(ctx)

			switch outcome {
			case "rejected":
				require.ErrorIs(t, err, assert.AnError)
			case "cancelled":
				require.ErrorIs(t, err, context.Canceled)
			default:
				require.NoError(t, err)
			}

			if outcome == "offline" {
				assert.Zero(t, requests)
				assert.Empty(t, fake.created)

				return
			}

			assert.Equal(t, 1, requests)
			assert.Equal(t, fake.created, fake.deleted)
			assert.Equal(t, 1, backend.cleaned)
		})
	}
}

//nolint:paralleltest // replaces the ephemeral backend factory
func TestPreparedEphemeralClusterStopsAtOfflineGate(t *testing.T) {
	fake := &fakeEphemeralProvisioner{}
	installEphemeralProvisioner(t, fake, nil)
	err := workload.ExportWithPreparedEphemeralCluster(
		t.Context(),
		newTestCommand(t),
		[]string{t.TempDir()},
		func(context.Context) error { return assert.AnError },
	)
	require.ErrorIs(t, err, assert.AnError)
	assert.Empty(t, fake.created)
}
