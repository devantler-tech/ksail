package cluster_test

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const implicitEKSCommandRegion = "ap-southeast-2"

// writeImplicitEKSCommandKubeconfig writes one eksctl-style context whose cluster entry
// points at server, so the selection either reaches the owned cluster or matches nothing.
func writeImplicitEKSCommandKubeconfig(t *testing.T, clusterName, server string) {
	t.Helper()

	contextName := fmt.Sprintf(
		"arn:aws:iam::123456789012:role/ksail-test@%s.%s.eksctl.io",
		clusterName,
		implicitEKSCommandRegion,
	)

	kubeconfig := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: %[1]s
  cluster:
    server: %[2]s
contexts:
- name: %[3]s
  context:
    cluster: %[1]s
    user: eksctl-user
current-context: %[3]s
users:
- name: eksctl-user
  user: {}
`, clusterName, server, contextName)

	require.NoError(t, os.WriteFile("kubeconfig", []byte(kubeconfig), 0o600))
}

func executeImplicitEKSCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()

	var output bytes.Buffer

	cmd.SetArgs(args)
	cmd.SetContext(t.Context())
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	err := cmd.Execute()

	return output.String(), err
}

// Update must stop before planning when no context reaches the owned EKS cluster: every
// drift probe would otherwise read nothing about the cluster it is about to change.
//
//nolint:paralleltest // mutates process environment, working directory, and shared hooks.
func TestEKSUpdateStopsWhenTheImplicitContextCannotBeSelected(t *testing.T) {
	clusterName := "ksail-eks-update-implicit-context-7837"
	setupStandaloneEKSLifecycleFixture(t, clusterName)
	writeImplicitEKSCommandKubeconfig(t, clusterName, "https://example.invalid")

	_, err := executeImplicitEKSCommand(t, cluster.NewUpdateCmd(), "--dry-run")
	require.ErrorContains(t, err, fmt.Sprintf(
		"no kubeconfig context matches EKS cluster %q", clusterName,
	))
}

// Update proceeds past selection when eksctl's context reaches the owned cluster.
//
//nolint:paralleltest // mutates process environment, working directory, and shared hooks.
func TestEKSUpdateSelectsTheImplicitEksctlContext(t *testing.T) {
	clusterName := "ksail-eks-update-implicit-context-match-7837"
	setupStandaloneEKSLifecycleFixture(t, clusterName)
	writeImplicitEKSCommandKubeconfig(
		t, clusterName, immutableEKSEndpoint(implicitEKSCommandRegion),
	)

	_, err := executeImplicitEKSCommand(t, cluster.NewUpdateCmd(), "--dry-run")

	// The fake cluster is unreachable, so update fails only once it dials the owned endpoint
	// through the selected context.
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "no kubeconfig context matches")
	assert.Contains(t, err.Error(), immutableEKSEndpointHost(implicitEKSCommandRegion))
}

// Diff keeps going when it cannot select the context but must record the live state as
// unknown, so its result can never read as a clean "no drift".
//
//nolint:paralleltest // mutates process environment, working directory, and shared hooks.
func TestEKSDiffReportsUnknownBaselineWhenTheImplicitContextCannotBeSelected(t *testing.T) {
	clusterName := "ksail-eks-diff-implicit-context-7837"
	setupStandaloneEKSLifecycleFixture(t, clusterName)
	writeImplicitEKSCommandKubeconfig(t, clusterName, "https://example.invalid")

	output, err := executeImplicitEKSCommand(t, cluster.NewDiffCmd(), "--output", "json")
	require.NoError(t, err, "output:\n%s", output)
	assert.Contains(t, output, "Cannot select the eksctl kubeconfig context")
	assert.Contains(t, output, `"cluster.connection.context"`)
}

// Diff selects eksctl's context when it reaches the owned cluster, so no unknown
// connection baseline is recorded.
//
//nolint:paralleltest // mutates process environment, working directory, and shared hooks.
func TestEKSDiffSelectsTheImplicitEksctlContext(t *testing.T) {
	clusterName := "ksail-eks-diff-implicit-context-match-7837"
	setupStandaloneEKSLifecycleFixture(t, clusterName)
	writeImplicitEKSCommandKubeconfig(
		t, clusterName, immutableEKSEndpoint(implicitEKSCommandRegion),
	)

	output, err := executeImplicitEKSCommand(t, cluster.NewDiffCmd(), "--output", "json")
	require.NoError(t, err, "output:\n%s", output)
	assert.NotContains(t, output, "Cannot select the eksctl kubeconfig context")
	assert.NotContains(t, output, `"cluster.connection.context"`)
	// Component detection dials the owned endpoint instead of the suffix-only fallback context.
	assert.NotContains(t, output, fmt.Sprintf(`context "%s.eksctl.io" does not exist`, clusterName))
	assert.Contains(t, output, immutableEKSEndpointHost(implicitEKSCommandRegion))
}

func immutableEKSEndpointHost(region string) string {
	return strings.TrimPrefix(immutableEKSEndpoint(region), "https://")
}
