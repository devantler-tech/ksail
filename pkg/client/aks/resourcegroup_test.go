package aks_test

import (
	"fmt"
	"testing"

	armcontainerservice "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v7"
	"github.com/devantler-tech/ksail/v7/pkg/client/aks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func clusterWithID(name, armID string) *armcontainerservice.ManagedCluster {
	cluster := &armcontainerservice.ManagedCluster{Name: new(name)}
	if armID != "" {
		cluster.ID = new(armID)
	}

	return cluster
}

// clusterInGroup builds a cluster whose ARM ID places it in the given
// resource group.
func clusterInGroup(name, group string) *armcontainerservice.ManagedCluster {
	armID := fmt.Sprintf(
		"/subscriptions/s/resourceGroups/%s/providers/Microsoft.ContainerService/managedClusters/%s",
		group,
		name,
	)

	return clusterWithID(name, armID)
}

func TestFindClusterResourceGroup_ParsesMatchingClusterID(t *testing.T) {
	t.Parallel()

	clusters := []*armcontainerservice.ManagedCluster{
		nil,
		{Name: nil},
		clusterInGroup("other", "rg-other"),
		clusterInGroup("dev", "rg-dev"),
	}

	group, found, err := aks.FindClusterResourceGroup(clusters, "dev")

	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "rg-dev", group)
}

func TestFindClusterResourceGroup_ReportsMissingCluster(t *testing.T) {
	t.Parallel()

	_, found, err := aks.FindClusterResourceGroup(nil, "dev")

	require.NoError(t, err)
	assert.False(t, found)
}

func TestFindClusterResourceGroup_FailsOnUnparsableID(t *testing.T) {
	t.Parallel()

	clusters := []*armcontainerservice.ManagedCluster{clusterWithID("dev", "")}

	_, _, err := aks.FindClusterResourceGroup(clusters, "dev")

	require.Error(t, err)
	assert.ErrorContains(t, err, "parse ARM ID of cluster dev")
}
