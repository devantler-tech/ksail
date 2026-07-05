package aks

import (
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	armcontainerservice "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v7"
)

// FindClusterResourceGroup scans clusters for one whose name matches
// clusterName and returns the resource group parsed from its ARM ID — how a
// cluster-scoped call resolves where to go when no resource group is
// configured. The boolean reports whether a matching cluster was found; a
// match whose ARM ID cannot be parsed returns an error.
func FindClusterResourceGroup(
	clusters []*armcontainerservice.ManagedCluster,
	clusterName string,
) (string, bool, error) {
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

		return resourceID.ResourceGroupName, true, nil
	}

	return "", false, nil
}
