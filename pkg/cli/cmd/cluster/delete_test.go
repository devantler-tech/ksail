package cluster_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	clusterpkg "github.com/devantler-tech/ksail/v5/pkg/cli/cmd/cluster"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	clustererrors "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/errors"
	"github.com/devantler-tech/ksail/v5/pkg/utils/timer"
	"github.com/docker/docker/client"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/samber/do/v2"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// fakeDeleteProvisioner is a fake provisioner for delete tests.
type fakeDeleteProvisioner struct {
	existsResult bool
	deleteErr    error
}

func (*fakeDeleteProvisioner) Create(context.Context, string) error { return nil }
func (f *fakeDeleteProvisioner) Delete(context.Context, string) error {
	return f.deleteErr
}
func (*fakeDeleteProvisioner) Start(context.Context, string) error { return nil }
func (*fakeDeleteProvisioner) Stop(context.Context, string) error  { return nil }
func (*fakeDeleteProvisioner) List(context.Context) ([]string, error) {
	return nil, nil
}

func (f *fakeDeleteProvisioner) Exists(context.Context, string) (bool, error) {
	return f.existsResult, nil
}

// fakeDeleteFactory creates a provisioner for delete tests.
type fakeDeleteFactory struct {
	existsResult bool
	deleteErr    error
}

//nolint:ireturn // Factory interface requires returning interface type
func (f fakeDeleteFactory) Create(
	_ context.Context,
	_ *v1alpha1.Cluster,
) (clusterprovisioner.ClusterProvisioner, any, error) {
	cfg := &v1alpha4.Cluster{Name: "test"}

	return &fakeDeleteProvisioner{
		existsResult: f.existsResult,
		deleteErr:    f.deleteErr,
	}, cfg, nil
}

func writeDeleteTestConfigFiles(t *testing.T, workingDir string, distribution string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(workingDir, 0o750))

	// Write kubeconfig (common to all distributions)
	require.NoError(t, os.WriteFile(
		filepath.Join(workingDir, "kubeconfig"),
		[]byte("apiVersion: v1\nkind: Config\nclusters: []\ncontexts: []\nusers: []\n"),
		0o600,
	))

	switch distribution {
	case "Kind":
		writeKindTestConfig(t, workingDir)
	case "K3d":
		writeK3dTestConfig(t, workingDir)
	case "Talos":
		writeTalosTestConfig(t, workingDir)
	}
}

func writeKindTestConfig(t *testing.T, workingDir string) {
	t.Helper()

	ksailYAML := `apiVersion: ksail.dev/v1alpha1
kind: Cluster
spec:
  cluster:
    distribution: Kind
    distributionConfig: kind.yaml
    metricsServer: Disabled
    localRegistry: Disabled
    connection:
      kubeconfig: ./kubeconfig
`
	require.NoError(t, os.WriteFile(
		filepath.Join(workingDir, "ksail.yaml"),
		[]byte(ksailYAML),
		0o600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(workingDir, "kind.yaml"),
		[]byte("kind: Cluster\napiVersion: kind.x-k8s.io/v1alpha4\nname: test\nnodes: []\n"),
		0o600,
	))
}

func writeK3dTestConfig(t *testing.T, workingDir string) {
	t.Helper()

	ksailYAML := `apiVersion: ksail.dev/v1alpha1
kind: Cluster
spec:
  cluster:
    distribution: K3d
    distributionConfig: k3d.yaml
    metricsServer: Disabled
    localRegistry: Disabled
    connection:
      kubeconfig: ./kubeconfig
`
	require.NoError(t, os.WriteFile(
		filepath.Join(workingDir, "ksail.yaml"),
		[]byte(ksailYAML),
		0o600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(workingDir, "k3d.yaml"),
		[]byte("apiVersion: k3d.io/v1alpha5\nkind: Simple\nmetadata:\n  name: test\n"),
		0o600,
	))
}

func writeTalosTestConfig(t *testing.T, workingDir string) {
	t.Helper()

	ksailYAML := `apiVersion: ksail.dev/v1alpha1
kind: Cluster
spec:
  cluster:
    distribution: Talos
    distributionConfig: talos.yaml
    metricsServer: Disabled
    localRegistry: Disabled
    connection:
      kubeconfig: ./kubeconfig
`
	require.NoError(t, os.WriteFile(
		filepath.Join(workingDir, "ksail.yaml"),
		[]byte(ksailYAML),
		0o600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(workingDir, "talos.yaml"),
		[]byte("clusterName: test\n"),
		0o600,
	))
}

func newDeleteTestRuntimeContainer(t *testing.T) *runtime.Runtime {
	t.Helper()

	return runtime.New(
		func(i runtime.Injector) error {
			do.Provide(i, func(runtime.Injector) (timer.Timer, error) {
				return timer.New(), nil
			})

			return nil
		},
		func(i runtime.Injector) error {
			do.Provide(i, func(runtime.Injector) (clusterprovisioner.Factory, error) {
				return fakeDeleteFactory{existsResult: true}, nil
			})

			return nil
		},
	)
}

// trimTrailingNewlineDelete removes a single trailing newline from snapshot output.
func trimTrailingNewlineDelete(s string) string {
	if len(s) > 0 && s[len(s)-1] == '\n' {
		return s[:len(s)-1]
	}

	return s
}

// TestDelete_ClusterExists_PrintsDeleteSuccess tests successful cluster deletion for all distributions.
//
//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir()
func TestDelete_ClusterExists_PrintsDeleteSuccess(t *testing.T) {
	testCases := []struct {
		name         string
		distribution string
	}{
		{name: "Kind", distribution: "Kind"},
		{name: "K3d", distribution: "K3d"},
		{name: "Talos", distribution: "Talos"},
	}

	for _, testCase := range testCases {
		//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir()
		t.Run(testCase.name, func(t *testing.T) {
			workingDir := t.TempDir()
			t.Chdir(workingDir)

			writeDeleteTestConfigFiles(t, workingDir, testCase.distribution)

			// Override cluster provisioner factory to use fake provisioner that returns success
			restoreFactory := clusterpkg.SetClusterProvisionerFactoryForTests(
				fakeDeleteFactory{existsResult: true, deleteErr: nil},
			)
			defer restoreFactory()

			// Override Docker client to call the callback for cleanup
			restoreDocker := clusterpkg.SetDockerClientInvokerForTests(
				func(_ *cobra.Command, fn func(client.APIClient) error) error {
					return fn(nil)
				},
			)
			defer restoreDocker()

			testRuntime := newDeleteTestRuntimeContainer(t)

			cmd := clusterpkg.NewDeleteCmd(testRuntime)

			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetContext(context.Background())

			err := cmd.Execute()
			require.NoError(t, err)

			snaps.MatchSnapshot(t, trimTrailingNewlineDelete(out.String()))
		})
	}
}

// TestDelete_ClusterNotFound_PrintsWarning tests deletion when cluster doesn't exist for all distributions.
//
//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir()
func TestDelete_ClusterNotFound_PrintsWarning(t *testing.T) {
	testCases := []struct {
		name         string
		distribution string
	}{
		{name: "Kind", distribution: "Kind"},
		{name: "K3d", distribution: "K3d"},
		{name: "Talos", distribution: "Talos"},
	}

	for _, testCase := range testCases {
		//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir()
		t.Run(testCase.name, func(t *testing.T) {
			workingDir := t.TempDir()
			t.Chdir(workingDir)

			writeDeleteTestConfigFiles(t, workingDir, testCase.distribution)

			// Override cluster provisioner factory to use fake provisioner that returns ErrClusterNotFound
			restoreFactory := clusterpkg.SetClusterProvisionerFactoryForTests(
				fakeDeleteFactory{existsResult: false, deleteErr: clustererrors.ErrClusterNotFound},
			)
			defer restoreFactory()

			// Override Docker client to call the callback for cleanup
			restoreDocker := clusterpkg.SetDockerClientInvokerForTests(
				func(_ *cobra.Command, fn func(client.APIClient) error) error {
					return fn(nil)
				},
			)
			defer restoreDocker()

			testRuntime := newDeleteTestRuntimeContainer(t)

			cmd := clusterpkg.NewDeleteCmd(testRuntime)

			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetContext(context.Background())

			err := cmd.Execute()
			require.NoError(t, err) // Should succeed even when cluster doesn't exist

			snaps.MatchSnapshot(t, trimTrailingNewlineDelete(out.String()))
		})
	}
}

// Ensure fake types satisfy interfaces at compile time.
var (
	_ clusterprovisioner.ClusterProvisioner = (*fakeDeleteProvisioner)(nil)
	_ clusterprovisioner.Factory            = (*fakeDeleteFactory)(nil)
)
