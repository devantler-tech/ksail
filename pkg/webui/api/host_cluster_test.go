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

// impostorCluster carries the reserved host-cluster label on an ordinary Cluster. The label is
// client-writable, so it proves nothing on its own: only the well-known name in the operator's own
// namespace is the real registration. Update and delete must stay available for this one.
func impostorCluster() *v1alpha1.Cluster {
	cluster := &v1alpha1.Cluster{}
	cluster.Name = "impostor"
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

// A forged label must not make an ordinary Cluster unupdatable. Matching the label alone would let
// any client that can set it lock its own Cluster out of the API permanently.
func TestUpdateAllowsClusterWithForgedHostLabel(t *testing.T) {
	t.Parallel()

	service := operator.NewCRClusterService(newClient(t, impostorCluster()))
	updater, ok := service.(api.ClusterUpdater)
	require.True(t, ok, "operator backend must implement ClusterUpdater")

	updated := impostorCluster()
	updated.Spec.Cluster.Distribution = v1alpha1.DistributionVCluster

	got, err := updater.Update(context.Background(), defaultNS, "impostor", updated)
	require.NoError(t, err)
	assert.Equal(t, v1alpha1.DistributionVCluster, got.Spec.Cluster.Distribution)
}

// The same forgery must not block deletion — otherwise setting one label is enough to make a
// Cluster undeletable through the API.
func TestDeleteAllowsClusterWithForgedHostLabel(t *testing.T) {
	t.Parallel()

	service := operator.NewCRClusterService(newClient(t, impostorCluster()))

	err := service.Delete(context.Background(), defaultNS, "impostor")
	require.NoError(t, err)

	_, getErr := service.Get(context.Background(), defaultNS, "impostor")
	require.Error(t, getErr, "the impostor must actually be gone after the delete")
}
