package cluster_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
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
