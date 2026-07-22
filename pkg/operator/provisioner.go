package operator

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v7/internal/controller"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	kindconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/kind"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
)

// ErrUnsupportedDistribution is returned when the operator is asked to provision a distribution it
// does not recognize.
var ErrUnsupportedDistribution = errors.New("unsupported distribution for operator")

// defaultAWSRegionEnvVar is the environment variable the operator reads the EKS region from when the
// cluster does not override it.
const defaultAWSRegionEnvVar = "AWS_REGION"

// defaultGCPProjectEnvVar / defaultGCPLocationEnvVar are the environment variables the operator
// reads the GKE project and location from when the cluster does not name its own
// (mirrors OptionsGCP's struct-tag defaults).
const (
	defaultGCPProjectEnvVar  = "GOOGLE_CLOUD_PROJECT"
	defaultGCPLocationEnvVar = "GOOGLE_CLOUD_LOCATION"
)

// defaultAzureSubscriptionIDEnvVar / defaultAzureResourceGroupEnvVar are the environment variables
// the operator reads the AKS subscription and resource group from when the cluster does not name
// its own (mirrors OptionsAzure's struct-tag defaults).
const (
	defaultAzureSubscriptionIDEnvVar = "AZURE_SUBSCRIPTION_ID"
	defaultAzureResourceGroupEnvVar  = "AZURE_RESOURCE_GROUP"
)

// BuildProvisioner returns a provisioner for the cluster's distribution and provider. Distribution
// and Provider follow the API's zero-value convention: an unset distribution means Vanilla and an
// unset provider means Docker (their default values serialize to empty via `omitzero`). EKS uses
// AWS. Every distribution × provider combination the factory supports is available.
//
// Provider requirements at runtime: the Docker provider needs an accessible Docker endpoint
// (DOCKER_HOST, or a mounted /var/run/docker.sock via the chart's operator.dockerSocket.enabled);
// the cloud providers (AWS, Hetzner, Omni) need their credentials in the operator's environment;
// the Kubernetes provider provisions nested clusters in the hub and needs the Gateway API CRDs.
//
// It satisfies controller.ProvisionerBuilder.
func BuildProvisioner(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
) (clusterprovisioner.Provisioner, error) {
	desired := cluster.DeepCopy()

	if desired.Spec.Cluster.Distribution == "" {
		desired.Spec.Cluster.Distribution = v1alpha1.DistributionVanilla
	}

	desired.Spec.Cluster.Provider = resolveProvider(desired)

	validateErr := desired.Spec.Cluster.Provider.ValidateForDistribution(
		desired.Spec.Cluster.Distribution,
	)
	if validateErr != nil {
		return nil, fmt.Errorf("validate distribution/provider: %w", validateErr)
	}

	distConfig, err := buildDistributionConfig(desired)
	if err != nil {
		return nil, err
	}

	factory := clusterprovisioner.DefaultFactory{DistributionConfig: distConfig}

	provisioner, _, err := factory.Create(ctx, desired)
	if err != nil {
		return nil, fmt.Errorf("create provisioner: %w", err)
	}

	return provisioner, nil
}

// resolveProvider returns the provider declared on the cluster, or the API default when unset:
// AWS for EKS (its only provider), Docker for everything else (Provider's zero value). The UI sends
// an explicit provider, so an unset provider only occurs for hand-written Cluster resources.
func resolveProvider(cluster *v1alpha1.Cluster) v1alpha1.Provider {
	if cluster.Spec.Cluster.Provider != "" {
		return cluster.Spec.Cluster.Provider
	}

	if cluster.Spec.Cluster.Distribution == v1alpha1.DistributionEKS {
		return v1alpha1.ProviderAWS
	}

	if cluster.Spec.Cluster.Distribution == v1alpha1.DistributionGKE {
		return v1alpha1.ProviderGCP
	}

	if cluster.Spec.Cluster.Distribution == v1alpha1.DistributionAKS {
		return v1alpha1.ProviderAzure
	}

	return v1alpha1.ProviderDocker
}

// buildDistributionConfig builds the in-memory distribution config the factory needs. Each config
// is named after the operator's provisioned name so multiple Cluster resources never collide on the
// same underlying cluster. An empty distribution defaults to Vanilla (the API's zero value).
func buildDistributionConfig(
	cluster *v1alpha1.Cluster,
) (*clusterprovisioner.DistributionConfig, error) {
	name := controller.ProvisionedName(cluster)

	distribution := cluster.Spec.Cluster.Distribution
	if distribution == "" {
		distribution = v1alpha1.DistributionVanilla
	}

	//nolint:exhaustive // K3s, VCluster, and KWOK are handled via SimpleDistributionConfig (default).
	switch distribution {
	case v1alpha1.DistributionVanilla:
		return &clusterprovisioner.DistributionConfig{
			Kind: kindconfigmanager.NewKindCluster(name, "", ""),
		}, nil
	case v1alpha1.DistributionTalos:
		// Honor the Kubernetes version pin / cap the default to the pinned Talos
		// version, matching the CLI so the operator never deploys an incompatible
		// Kubernetes version.
		kubernetesVersion := talosconfigmanager.ResolveKubernetesVersion(
			cluster.Spec.Cluster.Talos.Version,
			cluster.Spec.Cluster.KubernetesVersion,
		)

		talosConfig, err := newTalosConfig(
			name,
			kubernetesVersion,
			cluster.Spec.Cluster.Talos.Version,
		)
		if err != nil {
			return nil, err
		}

		return &clusterprovisioner.DistributionConfig{Talos: talosConfig}, nil
	case v1alpha1.DistributionEKS:
		return &clusterprovisioner.DistributionConfig{
			EKS: &clusterprovisioner.EKSConfig{Name: name, Region: awsRegion(cluster)},
		}, nil
	case v1alpha1.DistributionGKE:
		return &clusterprovisioner.DistributionConfig{
			GKE: &clusterprovisioner.GKEConfig{
				Name:     name,
				Project:  gcpProject(cluster),
				Location: gcpLocation(cluster),
			},
		}, nil
	case v1alpha1.DistributionAKS:
		return &clusterprovisioner.DistributionConfig{
			AKS: &clusterprovisioner.AKSConfig{
				Name:           name,
				SubscriptionID: azureSubscriptionID(cluster),
				ResourceGroup:  azureResourceGroup(cluster),
			},
		}, nil
	default:
		// K3s, VCluster, KWOK need only the name (shared with the local `ksail open web` backend).
		config := clusterprovisioner.SimpleDistributionConfig(distribution, name)
		if config != nil {
			return config, nil
		}

		return nil, fmt.Errorf("%w: %q", ErrUnsupportedDistribution, distribution)
	}
}

// newTalosConfig builds a default Talos config bundle named after the provisioned cluster, at the
// given Kubernetes version. The cluster name is baked into the PKI, so it must be set via WithName
// (which regenerates the bundle).
func newTalosConfig(
	name, kubernetesVersion, talosVersion string,
) (*talosconfigmanager.Configs, error) {
	versionContract, err := talosconfigmanager.ParseVersionContract(talosVersion)
	if err != nil {
		return nil, fmt.Errorf("resolve Talos version contract: %w", err)
	}

	config, err := talosconfigmanager.NewDefaultConfigsWithVersionContractAndName(
		kubernetesVersion,
		name,
		versionContract,
	)
	if err != nil {
		return nil, fmt.Errorf("build Talos config: %w", err)
	}

	return config, nil
}

// resolveEnvVar reads the environment variable named by envVar, falling back to
// defaultEnvVar when the cluster spec does not name one.
func resolveEnvVar(envVar, defaultEnvVar string) string {
	if envVar == "" {
		envVar = defaultEnvVar
	}

	return os.Getenv(envVar)
}

// awsRegion resolves the EKS region from the environment variable named by the cluster's AWS
// options (default AWS_REGION). An empty result lets the eksctl client surface a clear error.
func awsRegion(cluster *v1alpha1.Cluster) string {
	return resolveEnvVar(cluster.Spec.Provider.AWS.RegionEnvVar, defaultAWSRegionEnvVar)
}

// gcpProject resolves the GKE project from the operator's fixed GOOGLE_CLOUD_PROJECT environment
// variable. Cluster resources cannot select arbitrary operator environment variables because
// reconciliation failures are reflected in public Cluster status conditions.
func gcpProject(_ *v1alpha1.Cluster) string {
	return resolveEnvVar("", defaultGCPProjectEnvVar)
}

// gcpLocation resolves the GKE location from the operator's fixed GOOGLE_CLOUD_LOCATION environment
// variable. An empty result leaves the location unpinned: reads resolve the cluster's own location,
// while create fails with a clear ErrLocationRequired.
func gcpLocation(_ *v1alpha1.Cluster) string {
	return resolveEnvVar("", defaultGCPLocationEnvVar)
}

// azureSubscriptionID resolves the AKS subscription from the environment variable named by the
// cluster's Azure options (default AZURE_SUBSCRIPTION_ID). An empty result lets the AKS client
// surface a clear ErrSubscriptionRequired.
func azureSubscriptionID(cluster *v1alpha1.Cluster) string {
	return resolveEnvVar(
		cluster.Spec.Provider.Azure.SubscriptionIDEnvVar, defaultAzureSubscriptionIDEnvVar,
	)
}

// azureResourceGroup resolves the AKS resource group from the environment variable named by the
// cluster's Azure options (default AZURE_RESOURCE_GROUP). An empty result leaves the resource
// group unpinned: reads resolve the cluster's own group, while create fails clearly without one.
func azureResourceGroup(cluster *v1alpha1.Cluster) string {
	return resolveEnvVar(
		cluster.Spec.Provider.Azure.ResourceGroupEnvVar, defaultAzureResourceGroupEnvVar,
	)
}
