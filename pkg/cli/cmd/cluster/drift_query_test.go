package cluster_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errDriftQueryFailed = errors.New("cluster unreachable")

// newDriftQueryFixture returns a command capturing its output and a context whose
// cluster pins a kubeconfig path and context, so a test can check what the query
// receives without reading a real cluster.
func newDriftQueryFixture(
	t *testing.T,
) (*cobra.Command, *bytes.Buffer, *localregistry.Context, string) {
	t.Helper()

	var out bytes.Buffer

	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())

	kubeconfigPath := filepath.Join(t.TempDir(), "kubeconfig")

	clusterCfg := v1alpha1.NewCluster()
	clusterCfg.Spec.Cluster.Connection.Kubeconfig = kubeconfigPath
	clusterCfg.Spec.Cluster.Connection.Context = "pinned-context"

	return cmd, &out, &localregistry.Context{ClusterCfg: clusterCfg}, kubeconfigPath
}

// TestQueryLiveDriftPassesTheResolvedTarget pins that a drift query runs against the
// configured kubeconfig and the context the update targets, and that its answer is
// returned without any warning.
func TestQueryLiveDriftPassesTheResolvedTarget(t *testing.T) {
	t.Parallel()

	cmd, out, ctx, kubeconfigPath := newDriftQueryFixture(t)

	got, queried := cluster.ExportQueryLiveDrift(cmd, ctx, "test", "query test state",
		func(_ context.Context, gotPath, gotContext string) (string, error) {
			assert.Equal(t, kubeconfigPath, gotPath)
			assert.Equal(t, "pinned-context", gotContext)

			return "observed", nil
		},
	)

	require.True(t, queried)
	assert.Equal(t, "observed", got)
	assert.Empty(t, out.String(), "a successful drift query must not warn")
}

// TestQueryLiveDriftWarnsAndSkipsOnQueryFailure pins the shared policy: a failed drift
// query prints one warning naming the query and reports the check as skipped, never as
// an error that would fail the update.
func TestQueryLiveDriftWarnsAndSkipsOnQueryFailure(t *testing.T) {
	t.Parallel()

	cmd, out, ctx, _ := newDriftQueryFixture(t)

	got, queried := cluster.ExportQueryLiveDrift(cmd, ctx, "test", "query test state",
		func(context.Context, string, string) (bool, error) {
			return true, errDriftQueryFailed
		},
	)

	assert.False(t, queried)
	assert.False(
		t,
		got,
		"a skipped check must return the zero value, not the failed query's answer",
	)
	assert.Equal(
		t,
		"⚠ Cannot query test state for drift detection: cluster unreachable\n",
		out.String(),
	)
}
