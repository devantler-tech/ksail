package clusterprovisioner

import (
	"errors"
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	eksctlclient "github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	awsprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/aws"
	eksprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/eks"
)

// ErrUnfrozenAWSResolution reports an identity-sensitive EKS factory call that supplied a mutable
// profile/default-chain selector instead of the concrete credential tuple verified by the guard.
var ErrUnfrozenAWSResolution = errors.New("EKS mutation credentials are not frozen")

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

	client, providerOptions, provisionerOptions, err := f.resolveEKSCredentialOptions(
		cluster.Spec.Provider.AWS,
	)
	if err != nil {
		return nil, nil, err
	}

	provisionerOptions = append(provisionerOptions,
		eksprovisioner.WithKubeconfigPath(eksConfig.KubeconfigPath),
	)
	if f.AWSOwnershipVerifier != nil {
		providerOptions = append(
			providerOptions,
			awsprovider.WithOwnershipVerifier(f.AWSOwnershipVerifier),
		)
		provisionerOptions = append(
			provisionerOptions,
			eksprovisioner.WithOwnershipVerifier(f.AWSOwnershipVerifier),
		)
	}

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

// resolveEKSCredentialOptions snapshots one AWS resolution and derives aligned
// eksctl, provider, and provisioner options.
func (f DefaultFactory) resolveEKSCredentialOptions(
	awsOptions v1alpha1.OptionsAWS,
) (*eksctlclient.Client, []awsprovider.Option, []eksprovisioner.Option, error) {
	if f.AWSOwnershipVerifier != nil && f.AWSResolution == nil {
		return nil, nil, nil, ErrUnfrozenAWSResolution
	}

	auth := credentials.ResolveAWS(credentials.NewAWSOptionsResolver(awsOptions))

	if f.AWSResolution != nil {
		if !f.AWSResolution.IsFrozen() {
			return nil, nil, nil, ErrUnfrozenAWSResolution
		}

		auth = *f.AWSResolution
	}

	eksctlOptions := credentials.OptionsForAWSChildEnvironment(
		auth,
		os.Environ(),
		eksctlclient.WithEnvironment,
		eksctlclient.RequireCredentialValues,
	)
	providerOptions := credentials.OptionsForFrozenAWSConfig(
		auth,
		awsprovider.WithAWSConfig,
		awsprovider.WithCredentialValues,
		awsprovider.RequireCredentialValues,
	)
	provisionerOptions := credentials.OptionsForFrozenAWSConfig(
		auth,
		eksprovisioner.WithAWSConfig,
		eksprovisioner.WithCredentialValues,
		eksprovisioner.RequireCredentialValues,
	)

	return eksctlclient.NewClient(eksctlOptions...), providerOptions, provisionerOptions, nil
}
