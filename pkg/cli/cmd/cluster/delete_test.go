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

// writeKubeconfigWithContext creates a kubeconfig file with the given current context.
func writeKubeconfigWithContext(t *testing.T, dir, currentContext string) string {
	t.Helper()

	kubeconfigContent := `apiVersion: v1
kind: Config
current-context: ` + currentContext + `
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: ` + currentContext + `
contexts:
- context:
    cluster: ` + currentContext + `
    user: ` + currentContext + `
  name: ` + currentContext + `
users:
- name: ` + currentContext + `
  user: {}
`
	kubeconfigPath := filepath.Join(dir, "kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfigPath, []byte(kubeconfigContent), 0o600))

	return kubeconfigPath
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
	)
}

// trimTrailingNewlineDelete removes a single trailing newline from snapshot output.
func trimTrailingNewlineDelete(s string) string {
	if len(s) > 0 && s[len(s)-1] == '\n' {
		return s[:len(s)-1]
	}

	return s
}

// setupContextBasedTest sets up a test environment for context-based detection tests.
// Returns a cleanup function that must be called with defer.
func setupContextBasedTest(
	t *testing.T,
	contextName string,
	existsResult bool,
	deleteErr error,
) (*runtime.Runtime, func()) {
	t.Helper()

	workingDir := t.TempDir()
	t.Chdir(workingDir)

	kubeconfigPath := writeKubeconfigWithContext(t, workingDir, contextName)
	t.Setenv("KUBECONFIG", kubeconfigPath)

	restoreFactory := clusterpkg.SetClusterProvisionerFactoryForTests(
		fakeDeleteFactory{existsResult: existsResult, deleteErr: deleteErr},
	)

	// Override Docker client to skip cleanup (no Docker in tests)
	restoreDocker := clusterpkg.SetDockerClientInvokerForTests(
		func(_ *cobra.Command, _ func(client.APIClient) error) error {
			return nil // Skip Docker operations in tests
		},
	)

	testRuntime := newDeleteTestRuntimeContainer(t)

	cleanup := func() {
		restoreDocker()
		restoreFactory()
	}

	return testRuntime, cleanup
}

// TestDelete_ContextBasedDetection_DeletesCluster tests that delete can detect
// distribution from kubeconfig context and delete the cluster successfully.
//
//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir() and t.Setenv() in helper
func TestDelete_ContextBasedDetection_DeletesCluster(t *testing.T) {
	testCases := []struct {
		name    string
		context string
	}{
		{name: "Kind_context_pattern", context: "kind-my-cluster"},
		{name: "K3d_context_pattern", context: "k3d-dev-cluster"},
		{name: "Talos_context_pattern", context: "admin@talos-homelab"},
	}

	for _, testCase := range testCases {
		//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir()
		t.Run(testCase.name, func(t *testing.T) {
			testRuntime, cleanup := setupContextBasedTest(t, testCase.context, true, nil)
			defer cleanup()

			cmd := clusterpkg.NewDeleteCmd(testRuntime)

			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetContext(context.Background())

			err := cmd.Execute()
			require.NoError(t, err)

			output := out.String()
			require.Contains(t, output, "cluster deleted")

			snaps.MatchSnapshot(t, trimTrailingNewlineDelete(output))
		})
	}
}

// TestDelete_ContextBasedDetection_ClusterNotFound tests that context-based detection
// correctly returns an error when the detected cluster doesn't exist.
//
//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir() and t.Setenv() in helper
func TestDelete_ContextBasedDetection_ClusterNotFound(t *testing.T) {
	testRuntime, cleanup := setupContextBasedTest(
		t,
		"kind-nonexistent",
		false,
		clustererrors.ErrClusterNotFound,
	)
	defer cleanup()

	cmd := clusterpkg.NewDeleteCmd(testRuntime)

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())

	err := cmd.Execute()
	require.Error(t, err)
	require.ErrorIs(t, err, clustererrors.ErrClusterNotFound)

	snaps.MatchSnapshot(t, trimTrailingNewlineDelete(out.String()))
}

// TestDelete_ContextBasedDetection_UnknownContextPattern tests that delete returns
// an error when the context doesn't match a known distribution pattern.
func TestDelete_ContextBasedDetection_UnknownContextPattern(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)

	kubeconfigPath := writeKubeconfigWithContext(t, workingDir, "docker-desktop")
	t.Setenv("KUBECONFIG", kubeconfigPath)

	// Override Docker client to skip cleanup (no Docker in tests)
	restoreDocker := clusterpkg.SetDockerClientInvokerForTests(
		func(_ *cobra.Command, _ func(client.APIClient) error) error {
			return nil // Skip Docker operations in tests
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
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to detect cluster")
}

// TestDelete_CommandFlags verifies that the delete command has only the expected flags.
func TestDelete_CommandFlags(t *testing.T) {
	t.Parallel()

	testRuntime := newDeleteTestRuntimeContainer(t)
	cmd := clusterpkg.NewDeleteCmd(testRuntime)

	// Verify expected flags exist
	contextFlag := cmd.Flags().Lookup("context")
	require.NotNil(t, contextFlag, "expected --context flag")
	require.Equal(t, "c", contextFlag.Shorthand)

	kubeconfigFlag := cmd.Flags().Lookup("kubeconfig")
	require.NotNil(t, kubeconfigFlag, "expected --kubeconfig flag")

	deleteStorageFlag := cmd.Flags().Lookup("delete-storage")
	require.NotNil(t, deleteStorageFlag, "expected --delete-storage flag")

	// Verify old flags do NOT exist
	distributionFlag := cmd.Flags().Lookup("distribution")
	require.Nil(t, distributionFlag, "unexpected --distribution flag (should be removed)")

	deleteVolumesFlag := cmd.Flags().Lookup("delete-volumes")
	require.Nil(
		t,
		deleteVolumesFlag,
		"unexpected --delete-volumes flag (renamed to --delete-storage)",
	)
}

// Ensure fake types satisfy interfaces at compile time.
var (
	_ clusterprovisioner.ClusterProvisioner = (*fakeDeleteProvisioner)(nil)
	_ clusterprovisioner.Factory            = (*fakeDeleteFactory)(nil)
)
