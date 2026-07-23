package cluster_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	cluster "github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/stretchr/testify/require"
)

// TestPersistRequiredEKSComponentState_FailsClosed proves an applied EKS
// component mutation cannot report success when its exact-region baseline
// cannot be persisted.
//
//nolint:paralleltest // creates a deliberate path obstruction under isolated test HOME
func TestPersistRequiredEKSComponentState_FailsClosed(t *testing.T) {
	const clusterName = "unwritable-component-state"

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	clustersDir := filepath.Join(home, ".ksail", "clusters")
	require.NoError(t, os.MkdirAll(clustersDir, 0o700))
	require.NoError(t, os.WriteFile(
		filepath.Join(clustersDir, clusterName),
		[]byte("blocked"),
		0o600,
	))

	ctx := &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{},
		EKSConfig: &clusterprovisioner.EKSConfig{
			Name:   clusterName,
			Region: "eu-north-1",
		},
	}
	ctx.ClusterCfg.Spec.Cluster.Distribution = v1alpha1.DistributionEKS
	ctx.ClusterCfg.Spec.Cluster.Provider = v1alpha1.ProviderAWS
	ctx.ClusterCfg.Spec.Cluster.LoadBalancer = v1alpha1.LoadBalancerEnabled
	ctx.ClusterCfg.Spec.Cluster.EKS.ExperimentalAWSLoadBalancerController = true

	err = cluster.ExportPersistRequiredEKSComponentState(ctx, clusterName)
	require.ErrorContains(t, err, "persist required EKS component state")
}
