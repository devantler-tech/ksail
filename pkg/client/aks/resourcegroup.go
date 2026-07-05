package aks

import (
	"context"
	"errors"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	armcontainerservice "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v7"
)

// ErrAmbiguousCluster reports a cluster name that matches clusters in more
// than one resource group, so a subscription-wide lookup cannot decide which
// cluster a cluster-scoped call should target.
var ErrAmbiguousCluster = errors.New("cluster name matches clusters in multiple resource groups")

// ClusterLister lists managed clusters in a resource group, or across the
// whole subscription when resourceGroup is empty — the slice of the AKS
// client surface resource-group resolution needs.
type ClusterLister interface {
	ListClusters(
		ctx context.Context,
		resourceGroup string,
	) ([]*armcontainerservice.ManagedCluster, error)
}

// ResolveClusterResourceGroup resolves the resource group cluster-scoped AKS
// calls should target: the pinned resource group when one is configured;
// otherwise the named cluster's own group, parsed from its ARM ID via a
// subscription-wide list. A missing cluster maps onto the caller's notFound
// sentinel, and a name matching clusters in different resource groups is
// ErrAmbiguousCluster rather than a silent first-match pick.
func ResolveClusterResourceGroup(
	ctx context.Context,
	lister ClusterLister,
	clusterName, pinned string,
	notFound error,
) (string, error) {
	if pinned != "" {
		return pinned, nil
	}

	clusters, err := lister.ListClusters(ctx, "")
	if err != nil {
		return "", fmt.Errorf("resolve cluster resource group: %w", err)
	}

	group, found, err := FindClusterResourceGroup(clusters, clusterName)
	if err != nil {
		return "", fmt.Errorf("resolve cluster resource group: %w", err)
	}

	if !found {
		return "", fmt.Errorf("%w: %s", notFound, clusterName)
	}

	return group, nil
}

// FindClusterResourceGroup scans clusters for the one whose name matches
// clusterName and returns the resource group parsed from its ARM ID. The
// boolean reports whether a matching cluster was found; a match whose ARM ID
// cannot be parsed returns an error, and matches spread across different
// resource groups return ErrAmbiguousCluster (AKS names are unique only
// within a resource group, so first-match would silently target the wrong
// cluster).
func FindClusterResourceGroup(
	clusters []*armcontainerservice.ManagedCluster,
	clusterName string,
) (string, bool, error) {
	var (
		group string
		found bool
	)

	for _, cluster := range clusters {
		if cluster == nil || cluster.Name == nil || *cluster.Name != clusterName {
			continue
		}

		var armID string
		if cluster.ID != nil {
			armID = *cluster.ID
		}

		resourceID, err := arm.ParseResourceID(armID)
		if err != nil {
			return "", false, fmt.Errorf("parse ARM ID of cluster %s: %w", clusterName, err)
		}

		if found && resourceID.ResourceGroupName != group {
			return "", false, fmt.Errorf(
				"%w: %s is in both %s and %s",
				ErrAmbiguousCluster, clusterName, group, resourceID.ResourceGroupName,
			)
		}

		group = resourceID.ResourceGroupName
		found = true
	}

	return group, found, nil
}
