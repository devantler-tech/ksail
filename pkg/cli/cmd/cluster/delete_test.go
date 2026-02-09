package cluster_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	clusterpkg "github.com/devantler-tech/ksail/v5/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/confirm"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v5/pkg/timer"
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

func newDeleteTestRuntimeContainer(t *testing.T) *di.Runtime {
	t.Helper()

	return di.New(
		func(i di.Injector) error {
			do.Provide(i, func(di.Injector) (timer.Timer, error) {
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
) (*di.Runtime, func()) {
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

	// Override TTY check to return false by default (non-interactive mode)
	// This ensures existing tests don't prompt for confirmation
	restoreTTY := confirm.SetTTYCheckerForTests(func() bool { return false })

	testRuntime := newDeleteTestRuntimeContainer(t)

	cleanup := func() {
		restoreTTY()
		restoreDocker()
		restoreFactory()
	}

	return testRuntime, cleanup
}

// TestDelete_ContextBasedDetection_DeletesCluster tests that delete can detect
// cluster from kubeconfig context and delete the cluster successfully.
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
		clustererr.ErrClusterNotFound,
	)
	defer cleanup()

	cmd := clusterpkg.NewDeleteCmd(testRuntime)

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())

	err := cmd.Execute()
	require.Error(t, err)
	require.ErrorIs(t, err, clustererr.ErrClusterNotFound)

	snaps.MatchSnapshot(t, trimTrailingNewlineDelete(out.String()))
}

// TestDelete_ContextBasedDetection_UnknownContextPattern tests that delete returns
// an error when the context doesn't match a known pattern.
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
	// Error should indicate cluster name is required
	require.Contains(t, err.Error(), "cluster name is required")
}

// TestDelete_CommandFlags verifies that the delete command has the expected flags.
func TestDelete_CommandFlags(t *testing.T) {
	t.Parallel()

	testRuntime := newDeleteTestRuntimeContainer(t)
	cmd := clusterpkg.NewDeleteCmd(testRuntime)

	// Verify expected new flags exist
	nameFlag := cmd.Flags().Lookup("name")
	require.NotNil(t, nameFlag, "expected --name flag")
	require.Equal(t, "n", nameFlag.Shorthand)

	providerFlag := cmd.Flags().Lookup("provider")
	require.NotNil(t, providerFlag, "expected --provider flag")
	require.Equal(t, "p", providerFlag.Shorthand)

	kubeconfigFlag := cmd.Flags().Lookup("kubeconfig")
	require.NotNil(t, kubeconfigFlag, "expected --kubeconfig flag")
	require.Equal(t, "k", kubeconfigFlag.Shorthand)

	deleteStorageFlag := cmd.Flags().Lookup("delete-storage")
	require.NotNil(t, deleteStorageFlag, "expected --delete-storage flag")

	forceFlag := cmd.Flags().Lookup("force")
	require.NotNil(t, forceFlag, "expected --force flag")
	require.Equal(t, "f", forceFlag.Shorthand)

	// Verify old flags do NOT exist
	contextFlag := cmd.Flags().Lookup("context")
	require.Nil(t, contextFlag, "unexpected --context flag (should be removed)")

	distributionFlag := cmd.Flags().Lookup("distribution")
	require.Nil(t, distributionFlag, "unexpected --distribution flag (should be removed)")
}

// TestDelete_Confirmation_Accepted tests that deletion proceeds when user confirms with "yes".
//
//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir() and t.Setenv() in helper
func TestDelete_Confirmation_Accepted(t *testing.T) {
	testRuntime, cleanup := setupContextBasedTest(t, "kind-my-cluster", true, nil)
	defer cleanup()

	// Mock stdin to return "yes"
	stdinReader := strings.NewReader("yes\n")

	restoreStdin := confirm.SetStdinReaderForTests(stdinReader)
	defer restoreStdin()

	// Mock TTY check to return true (simulates interactive terminal)
	restoreTTY := confirm.SetTTYCheckerForTests(func() bool { return true })
	defer restoreTTY()

	cmd := clusterpkg.NewDeleteCmd(testRuntime)

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())

	err := cmd.Execute()
	require.NoError(t, err)

	output := out.String()
	require.Contains(t, output, "cluster deleted")
	require.Contains(t, output, "The following resources will be deleted")

	snaps.MatchSnapshot(t, trimTrailingNewlineDelete(output))
}

// TestDelete_Confirmation_Denied tests that deletion is cancelled when user does not confirm.
//
//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir() and t.Setenv() in helper
func TestDelete_Confirmation_Denied(t *testing.T) {
	testRuntime, cleanup := setupContextBasedTest(t, "kind-my-cluster", true, nil)
	defer cleanup()

	// Mock stdin to return "no"
	stdinReader := strings.NewReader("no\n")

	restoreStdin := confirm.SetStdinReaderForTests(stdinReader)
	defer restoreStdin()

	// Mock TTY check to return true (simulates interactive terminal)
	restoreTTY := confirm.SetTTYCheckerForTests(func() bool { return true })
	defer restoreTTY()

	cmd := clusterpkg.NewDeleteCmd(testRuntime)

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())

	err := cmd.Execute()
	require.Error(t, err)
	require.ErrorIs(t, err, confirm.ErrDeletionCancelled)

	output := out.String()
	require.Contains(t, output, "The following resources will be deleted")
	require.NotContains(t, output, "cluster deleted")

	snaps.MatchSnapshot(t, trimTrailingNewlineDelete(output))
}

// TestDelete_ForceFlag_SkipsConfirmation tests that --force flag bypasses the confirmation prompt.
//
//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir() and t.Setenv() in helper
func TestDelete_ForceFlag_SkipsConfirmation(t *testing.T) {
	testRuntime, cleanup := setupContextBasedTest(t, "kind-my-cluster", true, nil)
	defer cleanup()

	// Mock TTY check to return true (simulates interactive terminal)
	// Note: stdin is NOT mocked - if prompt runs, it will fail to read
	restoreTTY := confirm.SetTTYCheckerForTests(func() bool { return true })
	defer restoreTTY()

	cmd := clusterpkg.NewDeleteCmd(testRuntime)
	cmd.SetArgs([]string{"--force"})

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())

	err := cmd.Execute()
	require.NoError(t, err)

	output := out.String()
	require.Contains(t, output, "cluster deleted")
	// Should NOT show confirmation preview when --force is used
	require.NotContains(t, output, "The following resources will be deleted")

	snaps.MatchSnapshot(t, trimTrailingNewlineDelete(output))
}

// TestDelete_NonTTY_SkipsConfirmation tests that non-TTY environments skip the confirmation prompt.
//
//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir() and t.Setenv() in helper
func TestDelete_NonTTY_SkipsConfirmation(t *testing.T) {
	testRuntime, cleanup := setupContextBasedTest(t, "kind-my-cluster", true, nil)
	defer cleanup()

	// Mock TTY check to return false (simulates CI/pipeline environment)
	restoreTTY := confirm.SetTTYCheckerForTests(func() bool { return false })
	defer restoreTTY()

	cmd := clusterpkg.NewDeleteCmd(testRuntime)

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())

	err := cmd.Execute()
	require.NoError(t, err)

	output := out.String()
	require.Contains(t, output, "cluster deleted")
	// Should NOT show confirmation preview when stdin is not a TTY
	require.NotContains(t, output, "The following resources will be deleted")

	snaps.MatchSnapshot(t, trimTrailingNewlineDelete(output))
}

// Ensure fake types satisfy interfaces at compile time.
var (
	_ clusterprovisioner.ClusterProvisioner = (*fakeDeleteProvisioner)(nil)
	_ clusterprovisioner.Factory            = (*fakeDeleteFactory)(nil)
)

// containerTestCase is a test case for IsClusterContainer.
type containerTestCase struct {
	name          string
	containerName string
	clusterName   string
	expected      bool
}

// getContainerTestCases returns test cases for IsClusterContainer.
func getContainerTestCases() []containerTestCase {
	return []containerTestCase{
		// Kind patterns
		{"kind_control_plane", "my-cluster-control-plane", "my-cluster", true},
		{"kind_worker", "my-cluster-worker", "my-cluster", true},
		{"kind_worker_numbered", "my-cluster-worker2", "my-cluster", true},
		// K3d patterns
		{"k3d_server", "k3d-my-cluster-server-0", "my-cluster", true},
		{"k3d_agent", "k3d-my-cluster-agent-0", "my-cluster", true},
		// Talos patterns
		{"talos_controlplane", "my-cluster-controlplane-1", "my-cluster", true},
		{"talos_worker", "my-cluster-worker-1", "my-cluster", true},
		// Non-matching
		{"different_cluster", "other-cluster-control-plane", "my-cluster", false},
		{"registry_container", "registry.localhost", "my-cluster", false},
		{"partial_match", "my-cluster-registry", "my-cluster", false},
		{"prefix_collision", "my-cluster-test-control-plane", "my-cluster", false},
	}
}

// TestIsClusterContainer tests the container name matching logic.
func TestIsClusterContainer(t *testing.T) {
	t.Parallel()

	for _, testCase := range getContainerTestCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := clusterpkg.IsClusterContainer(testCase.containerName, testCase.clusterName)
			require.Equal(t, testCase.expected, result)
		})
	}
}
