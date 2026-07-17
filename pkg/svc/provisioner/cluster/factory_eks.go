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

	// The Updater capability is discovered by type assertion, so the
	// experimental in-place update path is gated at construction: without the
	// opt-in the orchestrator sees no Updater and keeps today's recreate flow.
	// A config path is required too — with no declared eksctl.yaml (e.g. the
	// operator's name/region-only construction) the updater would have no
	// source of truth and silently report zero changes, which is worse than
	// falling back to the existing flow.
	if cluster.Spec.Cluster.EKS.ExperimentalInPlaceUpdates && eksConfig.ConfigPath != "" {
		return eksprovisioner.NewUpdatableProvisioner(provisioner), eksConfig, nil
	}

	return provisioner, eksConfig, nil
}

// resolveEKSCredentialOptions snapshots one AWS resolution and derives aligned
// eksctl, provider, and provisioner options.
func resolveEKSCredentialOptions(
	awsOptions v1alpha1.OptionsAWS,
) (*eksctlclient.Client, []awsprovider.Option, []eksprovisioner.Option) {
	auth, eksctlOptions, providerOptions := credentials.ResolveAWSClientOptions(
		credentials.NewAWSOptionsResolver(awsOptions),
		os.Environ(),
		eksctlclient.WithEnvironment,
		eksctlclient.RequireCredentialValues,
		awsprovider.WithCredentialValues,
		awsprovider.RequireCredentialValues,
	)
	provisionerOptions := credentials.OptionsForAWSResolution(
		auth, eksprovisioner.WithCredentialValues, eksprovisioner.RequireCredentialValues,
	)

	return eksctlclient.NewClient(eksctlOptions...), providerOptions, provisionerOptions
}
