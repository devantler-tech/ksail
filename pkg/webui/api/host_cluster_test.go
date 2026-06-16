package api_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/operator"
	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hostCluster mirrors the operator's self-registration of the cluster it runs on: the well-known
// host name with the reserved label and an empty spec.
func hostCluster() *v1alpha1.Cluster {
	cluster := &v1alpha1.Cluster{}
	cluster.Name = "host"
	cluster.Namespace = defaultNS
	cluster.Labels = map[string]string{v1alpha1.HostClusterLabel: "true"}

	return cluster
}

func TestCreateRejectsReservedHostClusterLabel(t *testing.T) {
	t.Parallel()

	service := operator.NewCRClusterService(newClient(t))

	_, err := service.Create(context.Background(), hostCluster())
	require.ErrorIs(t, err, api.ErrHostClusterProtected)
}

func TestUpdateRejectsHostCluster(t *testing.T) {
	t.Parallel()

	service := operator.NewCRClusterService(newClient(t, hostCluster()))
	updater, ok := service.(api.ClusterUpdater)
	require.True(t, ok, "operator backend must implement ClusterUpdater")

	updated := hostCluster()
	updated.Spec.Cluster.Distribution = v1alpha1.DistributionVCluster

	_, err := updater.Update(context.Background(), defaultNS, "host", updated)
	require.ErrorIs(t, err, api.ErrHostClusterProtected)
}

func TestDeleteRejectsHostCluster(t *testing.T) {
	t.Parallel()

	hub := newClient(t, hostCluster())
	service := operator.NewCRClusterService(hub)

	err := service.Delete(context.Background(), defaultNS, "host")
	require.ErrorIs(t, err, api.ErrHostClusterProtected)

	// The registration must still exist after the rejected delete.
	still, getErr := service.Get(context.Background(), defaultNS, "host")
	require.NoError(t, getErr)
	assert.True(t, still.IsHostCluster())
}
