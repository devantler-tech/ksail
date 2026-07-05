package aks_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	armcontainerservice "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v7"
	"github.com/devantler-tech/ksail/v7/pkg/client/aks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func clusterWithID(name, armID string) *armcontainerservice.ManagedCluster {
	cluster := &armcontainerservice.ManagedCluster{Name: &name}
	if armID != "" {
		cluster.ID = &armID
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

func TestFindClusterResourceGroup_FailsOnAmbiguousName(t *testing.T) {
	t.Parallel()

	clusters := []*armcontainerservice.ManagedCluster{
		clusterInGroup("dev", "rg-one"),
		clusterInGroup("dev", "rg-two"),
	}

	_, _, err := aks.FindClusterResourceGroup(clusters, "dev")

	require.ErrorIs(t, err, aks.ErrAmbiguousCluster)
	require.ErrorContains(t, err, "rg-one")
	require.ErrorContains(t, err, "rg-two")
}

func TestFindClusterResourceGroup_ToleratesDuplicateInSameGroup(t *testing.T) {
	t.Parallel()

	clusters := []*armcontainerservice.ManagedCluster{
		clusterInGroup("dev", "rg-dev"),
		clusterInGroup("dev", "rg-dev"),
	}

	group, found, err := aks.FindClusterResourceGroup(clusters, "dev")

	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "rg-dev", group)
}

// listerFunc adapts a function to the ClusterLister interface.
type listerFunc func(
	ctx context.Context,
	resourceGroup string,
) ([]*armcontainerservice.ManagedCluster, error)

func (f listerFunc) ListClusters(
	ctx context.Context,
	resourceGroup string,
) ([]*armcontainerservice.ManagedCluster, error) {
	return f(ctx, resourceGroup)
}

func TestResolveClusterResourceGroup_ListsSubscriptionWide(t *testing.T) {
	t.Parallel()

	lister := listerFunc(func(
		_ context.Context,
		resourceGroup string,
	) ([]*armcontainerservice.ManagedCluster, error) {
		assert.Empty(t, resourceGroup)

		return []*armcontainerservice.ManagedCluster{clusterInGroup("dev", "rg-dev")}, nil
	})

	group, err := aks.ResolveClusterResourceGroup(t.Context(), lister, "dev", "", errNotFound)

	require.NoError(t, err)
	assert.Equal(t, "rg-dev", group)
}

func TestResolveClusterResourceGroup_PrefersPinnedGroup(t *testing.T) {
	t.Parallel()

	lister := listerFunc(func(
		_ context.Context,
		_ string,
	) ([]*armcontainerservice.ManagedCluster, error) {
		t.Fatal("a pinned resource group must not trigger a list")

		return nil, nil
	})

	group, err := aks.ResolveClusterResourceGroup(
		t.Context(), lister, "dev", "rg-pinned", errNotFound,
	)

	require.NoError(t, err)
	assert.Equal(t, "rg-pinned", group)
}

func TestResolveClusterResourceGroup_WrapsListError(t *testing.T) {
	t.Parallel()

	lister := listerFunc(func(
		_ context.Context,
		_ string,
	) ([]*armcontainerservice.ManagedCluster, error) {
		return nil, errList
	})

	_, err := aks.ResolveClusterResourceGroup(t.Context(), lister, "dev", "", errNotFound)

	require.ErrorIs(t, err, errList)
	assert.ErrorContains(t, err, "resolve cluster resource group")
}

func TestResolveClusterResourceGroup_MapsMissingClusterToSentinel(t *testing.T) {
	t.Parallel()

	lister := listerFunc(func(
		_ context.Context,
		_ string,
	) ([]*armcontainerservice.ManagedCluster, error) {
		return []*armcontainerservice.ManagedCluster{clusterInGroup("other", "rg-other")}, nil
	})

	_, err := aks.ResolveClusterResourceGroup(t.Context(), lister, "dev", "", errNotFound)

	require.ErrorIs(t, err, errNotFound)
	assert.ErrorContains(t, err, "dev")
}

var (
	errList     = errors.New("list failed")
	errNotFound = errors.New("cluster not found")
)
