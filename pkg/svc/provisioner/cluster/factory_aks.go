package clusterprovisioner

import (
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	aksclient "github.com/devantler-tech/ksail/v7/pkg/client/aks"
	azureprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/azure"
	aksprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/aks"
)

// createAKSProvisioner wires the AKS cluster provisioner: an AKS SDK client
// (DefaultAzureCredential chain) plus the Azure infrastructure provider for
// the managed cluster's native Start/Stop. Client construction is local — no
// Azure call happens until an operation runs.
func (f DefaultFactory) createAKSProvisioner(
	_ *v1alpha1.Cluster,
) (Provisioner, any, error) {
	if f.DistributionConfig.AKS == nil {
		return nil, nil, fmt.Errorf(
			"aks config is required for AKS distribution: %w",
			ErrMissingDistributionConfig,
		)
	}

	aksConfig := f.DistributionConfig.AKS

	client, err := aksclient.NewClient(aksConfig.SubscriptionID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create AKS client: %w", err)
	}

	infraProvider, err := azureprovider.NewProvider(client, aksConfig.ResourceGroup)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Azure provider: %w", err)
	}

	provisioner, err := aksprovisioner.NewProvisioner(
		aksConfig.Name,
		aksConfig.ResourceGroup,
		aksConfig.ClusterSpec,
		client,
		infraProvider,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create AKS provisioner: %w", err)
	}

	return provisioner, aksConfig, nil
}
