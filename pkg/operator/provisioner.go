package operator

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v7/internal/controller"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	k3dconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/k3d"
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

	switch distribution {
	case v1alpha1.DistributionVanilla:
		return &clusterprovisioner.DistributionConfig{
			Kind: kindconfigmanager.NewKindCluster(name, "", ""),
		}, nil
	case v1alpha1.DistributionK3s:
		return &clusterprovisioner.DistributionConfig{
			K3d: k3dconfigmanager.NewK3dSimpleConfig(name, "", ""),
		}, nil
	case v1alpha1.DistributionTalos:
		talosConfig, err := newTalosConfig(name)
		if err != nil {
			return nil, err
		}

		return &clusterprovisioner.DistributionConfig{Talos: talosConfig}, nil
	case v1alpha1.DistributionVCluster:
		return &clusterprovisioner.DistributionConfig{
			VCluster: &clusterprovisioner.VClusterConfig{Name: name},
		}, nil
	case v1alpha1.DistributionKWOK:
		return &clusterprovisioner.DistributionConfig{
			KWOK: &clusterprovisioner.KWOKConfig{Name: name},
		}, nil
	case v1alpha1.DistributionEKS:
		return &clusterprovisioner.DistributionConfig{
			EKS: &clusterprovisioner.EKSConfig{Name: name, Region: awsRegion(cluster)},
		}, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedDistribution, distribution)
	}
}

// newTalosConfig builds a default Talos config bundle named after the provisioned cluster. The
// cluster name is baked into the PKI, so it must be set via WithName (which regenerates the bundle).
func newTalosConfig(name string) (*talosconfigmanager.Configs, error) {
	configs, err := talosconfigmanager.NewDefaultConfigs()
	if err != nil {
		return nil, fmt.Errorf("build talos config: %w", err)
	}

	named, err := configs.WithName(name)
	if err != nil {
		return nil, fmt.Errorf("name talos config: %w", err)
	}

	return named, nil
}

// awsRegion resolves the EKS region from the environment variable named by the cluster's AWS
// options (default AWS_REGION). An empty result lets the eksctl client surface a clear error.
func awsRegion(cluster *v1alpha1.Cluster) string {
	envVar := cluster.Spec.Provider.AWS.RegionEnvVar
	if envVar == "" {
		envVar = defaultAWSRegionEnvVar
	}

	return os.Getenv(envVar)
}
