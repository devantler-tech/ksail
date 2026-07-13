package clusterprovisioner

import (
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	eksctlclient "github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	awsprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/aws"
	eksprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/eks"
)

func (f DefaultFactory) createEKSProvisioner(
	_ *v1alpha1.Cluster,
) (Provisioner, any, error) {
	if f.DistributionConfig.EKS == nil {
		return nil, nil, fmt.Errorf(
			"eks config is required for EKS distribution: %w",
			ErrMissingDistributionConfig,
		)
	}

	eksConfig := f.DistributionConfig.EKS
	client := eksctlclient.NewClient()

	infraProvider, err := awsprovider.NewProvider(client, eksConfig.Region)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create AWS provider: %w", err)
	}

	provisioner, err := eksprovisioner.NewProvisioner(
		eksConfig.Name,
		eksConfig.Region,
		eksConfig.ConfigPath,
		client,
		infraProvider,
		eksprovisioner.WithKubeconfigPath(eksConfig.KubeconfigPath),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create EKS provisioner: %w", err)
	}

	return provisioner, eksConfig, nil
}
