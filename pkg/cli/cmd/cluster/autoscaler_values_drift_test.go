package cluster_test

import (
	"context"
	"errors"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errStopAfterCapture = errors.New("stop after capturing the probe config")

// TestCheckAutoscalerValuesDrift_TargetsTheResolvedContext pins the probe to
// the context every other drift probe resolves. With no pinned context the
// Helm client would otherwise read the kubeconfig's current-context and compare
// (then upgrade) an unrelated cluster's autoscaler (ksail#7366 review).
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckAutoscalerValuesDrift_TargetsTheResolvedContext(t *testing.T) {
	cfg := &v1alpha1.Cluster{}
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionTalos
	cfg.Spec.Cluster.Provider = v1alpha1.ProviderHetzner
	cfg.Spec.Cluster.Autoscaler.Node.Enabled = v1alpha1.NodeAutoscalerEnabledEnabled

	regCtx := &localregistry.Context{ClusterCfg: cfg}
	want := cluster.ExportResolveKubeContext(regCtx)
	require.NotEmpty(t, want, "the test needs a derived context to compare against")

	var probed string

	restore := cluster.SetClusterAutoscalerInstallerFactoryForTests(
		func(probeCfg *v1alpha1.Cluster) (installer.Installer, error) {
			probed = probeCfg.Spec.Cluster.Connection.Context

			return nil, errStopAfterCapture
		},
	)
	t.Cleanup(restore)

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportCheckAutoscalerValuesDrift(cmd, regCtx, diff)

	assert.Equal(t, want, probed, "the probe must use the resolved kube context")
	assert.Empty(t, cfg.Spec.Cluster.Connection.Context,
		"pinning the probe context must not mutate the caller's config")
	assert.Zero(t, diff.TotalChanges(), "a failed probe must not report drift")
}
