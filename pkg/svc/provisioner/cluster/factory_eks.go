package clusterprovisioner

import (
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	eksctlclient "github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	awsprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/aws"
	eksprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/eks"
)

func (f DefaultFactory) createEKSProvisioner(
	cluster *v1alpha1.Cluster,
) (Provisioner, any, error) {
	if f.DistributionConfig.EKS == nil {
		return nil, nil, fmt.Errorf(
			"eks config is required for EKS distribution: %w",
			ErrMissingDistributionConfig,
		)
	}

	eksConfig := f.DistributionConfig.EKS
	client, providerOptions, provisionerOptions := resolveEKSCredentialOptions(
		cluster.Spec.Provider.AWS,
	)
	provisionerOptions = append(provisionerOptions,
		eksprovisioner.WithKubeconfigPath(eksConfig.KubeconfigPath),
	)

	infraProvider, err := awsprovider.NewProvider(
		client,
		eksConfig.Region,
		providerOptions...,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create AWS provider: %w", err)
	}

	provisioner, err := eksprovisioner.NewProvisioner(
		eksConfig.Name,
		eksConfig.Region,
		eksConfig.ConfigPath,
		client,
		infraProvider,
		provisionerOptions...,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create EKS provisioner: %w", err)
	}

	return provisioner, eksConfig, nil
}

func resolveEKSCredentialOptions(
	awsOptions v1alpha1.OptionsAWS,
) (*eksctlclient.Client, []awsprovider.Option, []eksprovisioner.Option) {
	auth := credentials.ResolveAWS(credentials.NewAWSOptionsResolver(awsOptions))
	eksctlOptions := credentials.OptionsForAWSChildEnvironment(
		auth, os.Environ(), eksctlclient.WithEnvironment, eksctlclient.RequireCredentialValues,
	)
	providerOptions := credentials.OptionsForAWSResolution(
		auth, awsprovider.WithCredentialValues, awsprovider.RequireCredentialValues,
	)
	provisionerOptions := credentials.OptionsForAWSResolution(
		auth, eksprovisioner.WithCredentialValues, eksprovisioner.RequireCredentialValues,
	)

	return eksctlclient.NewClient(eksctlOptions...), providerOptions, provisionerOptions
}
