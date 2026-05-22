package operator

import (
	"context"
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v7/internal/controller"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
)

// ErrUnsupportedDistribution is returned when the operator is asked to provision a
// distribution it does not yet support in-cluster.
var ErrUnsupportedDistribution = errors.New("unsupported distribution for operator")

// BuildProvisioner returns a provisioner that creates the cluster inside the hub cluster the
// operator runs in. It forces the Kubernetes provider and builds the distribution config from
// the Cluster resource. It satisfies controller.ProvisionerBuilder.
func BuildProvisioner(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
) (clusterprovisioner.Provisioner, error) {
	desired := cluster.DeepCopy()
	// The operator provisions clusters in-cluster, regardless of the provider declared for
	// local (CLI) workflows.
	desired.Spec.Cluster.Provider = v1alpha1.ProviderKubernetes

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

// buildDistributionConfig derives the pre-loaded distribution config the factory needs.
// M2 supports the VCluster distribution; additional distributions are added in later milestones.
func buildDistributionConfig(
	cluster *v1alpha1.Cluster,
) (*clusterprovisioner.DistributionConfig, error) {
	distribution := cluster.Spec.Cluster.Distribution
	if distribution == "" {
		distribution = v1alpha1.DistributionVCluster
	}

	if distribution == v1alpha1.DistributionVCluster {
		return &clusterprovisioner.DistributionConfig{
			VCluster: &clusterprovisioner.VClusterConfig{Name: controller.ProvisionedName(cluster)},
		}, nil
	}

	return nil, fmt.Errorf(
		"%w: %q (operator currently supports VCluster)",
		ErrUnsupportedDistribution,
		distribution,
	)
}
