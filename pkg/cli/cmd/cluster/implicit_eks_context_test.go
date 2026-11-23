package cluster_test

import (
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The diff/update probes of an existing EKS cluster with an implicit context must reach the
// context eksctl wrote, not the suffix-only "<name>.eksctl.io" that never exists (ksail#7837).
func TestResolveImplicitEKSKubeContext_SelectsTheEksctlContext(t *testing.T) {
	t.Parallel()

	const sessionMatch = "ksail-system-test-eks-39005482922@st-eks.eu-west-1.eksctl.io"

	ctx := newImplicitEKSPostCreateContext(writeEKSContextKubeconfig(
		t, "unrelated-context",
		eksContextFixture{name: "unrelated-context"},
		eksContextFixture{name: sessionMatch, server: eksContextTestEndpoint},
	))

	// Negative control: without the fix every probe targets a context the kubeconfig lacks.
	require.Equal(t, "st-eks.eksctl.io", cluster.ExportResolveKubeContext(ctx))

	require.NoError(t, cluster.ExportResolveImplicitEKSKubeContext(ctx))
	assert.Equal(t, sessionMatch, cluster.ExportResolveKubeContext(ctx))
	require.NoError(t, cluster.ExportEnsureConfiguredContextResolvable(ctx.ClusterCfg))
}

func TestResolveImplicitEKSKubeContext_FailsClosedOnAnotherClusterOfTheSameName(t *testing.T) {
	t.Parallel()

	const sessionMatch = "operator@st-eks.eu-west-1.eksctl.io"

	ctx := newImplicitEKSPostCreateContext(writeEKSContextKubeconfig(
		t, sessionMatch,
		eksContextFixture{name: sessionMatch, server: eksContextTestOtherEndpoint},
	))

	err := cluster.ExportResolveImplicitEKSKubeContext(ctx)
	require.ErrorContains(t, err, `no kubeconfig context matches EKS cluster "st-eks"`)
	assert.Empty(t, ctx.ClusterCfg.Spec.Cluster.Connection.Context)
}

func TestResolveImplicitEKSKubeContext_LeavesPinnedAndOtherDistributionsAlone(t *testing.T) {
	t.Parallel()

	pinned := newImplicitEKSPostCreateContext("/nonexistent/kubeconfig")
	pinned.ClusterCfg.Spec.Cluster.Connection.Context = "operator@st-eks.eu-west-1.eksctl.io"

	require.NoError(t, cluster.ExportResolveImplicitEKSKubeContext(pinned))
	assert.Equal(t, "operator@st-eks.eu-west-1.eksctl.io",
		pinned.ClusterCfg.Spec.Cluster.Connection.Context)

	vanilla := newImplicitEKSPostCreateContext("/nonexistent/kubeconfig")
	vanilla.ClusterCfg.Spec.Cluster.Distribution = v1alpha1.DistributionVanilla
	vanilla.ClusterCfg.Spec.Cluster.Provider = v1alpha1.ProviderDocker

	require.NoError(t, cluster.ExportResolveImplicitEKSKubeContext(vanilla))
	assert.Empty(t, vanilla.ClusterCfg.Spec.Cluster.Connection.Context)
}

var errContextUnselectable = errors.New("no kubeconfig context matches")

// A context diff cannot select must never let it report "no drift" (ksail#7854 review).
func TestRecordUnselectableKubeContext_MarksTheBaselineUnknown(t *testing.T) {
	t.Parallel()

	clean := &clusterupdate.UpdateResult{}
	cluster.ExportRecordUnselectableKubeContext(clean, nil)
	assert.False(t, clean.HasUnknownBaseline(),
		"a selected context must not add an unknown baseline")

	failed := &clusterupdate.UpdateResult{}
	cluster.ExportRecordUnselectableKubeContext(failed, errContextUnselectable)
	require.True(t, failed.HasUnknownBaseline())
	require.Len(t, failed.UnknownBaseline, 1)
	assert.Equal(t, "cluster.connection.context", failed.UnknownBaseline[0].Field)
	assert.Equal(t, clusterupdate.UnknownBaselineValue, failed.UnknownBaseline[0].OldValue)
}

// The region environment variable overrides eks.yaml for diff/update exactly as it does for
// create, so the context selection targets the region the cluster actually runs in (ksail#7854).
func TestResolveImplicitEKSKubeContext_HonoursTheRegionEnvironmentVariable(t *testing.T) {
	t.Setenv("AWS_REGION", "eu-west-1")

	const sessionMatch = "operator@st-eks.eu-west-1.eksctl.io"

	ctx := newImplicitEKSPostCreateContext(writeEKSContextKubeconfig(
		t, sessionMatch,
		eksContextFixture{name: sessionMatch, server: eksContextTestEndpoint},
	))
	ctx.EKSConfig.Region = "us-east-1"

	require.NoError(t, cluster.ExportResolveImplicitEKSKubeContext(ctx))
	assert.Equal(t, "eu-west-1", ctx.EKSConfig.Region)
	assert.Equal(t, sessionMatch, cluster.ExportResolveKubeContext(ctx))
}
