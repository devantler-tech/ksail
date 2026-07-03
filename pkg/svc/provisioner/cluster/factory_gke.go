package clusterprovisioner

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	gkeclient "github.com/devantler-tech/ksail/v7/pkg/client/gke"
	gcpprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/gcp"
	gkeprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/gke"
)

// createGKEProvisioner wires the GKE cluster provisioner: a GKE SDK client
// (Application Default Credentials) plus the GCP infrastructure provider for
// Start/Stop node-pool scaling. The context is used for the SDK client dial
// only; per-operation contexts are passed by the provisioner's methods.
func (f DefaultFactory) createGKEProvisioner(
	ctx context.Context,
	_ *v1alpha1.Cluster,
) (Provisioner, any, error) {
	if f.DistributionConfig.GKE == nil {
		return nil, nil, fmt.Errorf(
			"gke config is required for GKE distribution: %w",
			ErrMissingDistributionConfig,
		)
	}

	gkeConfig := f.DistributionConfig.GKE

	client, err := gkeclient.NewClient(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create GKE client: %w", err)
	}

	infraProvider, err := gcpprovider.NewProvider(client, gkeConfig.Project, gkeConfig.Location)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create GCP provider: %w", err)
	}

	provisioner, err := gkeprovisioner.NewProvisioner(
		gkeConfig.Name,
		gkeConfig.Project,
		gkeConfig.Location,
		gkeConfig.ClusterSpec,
		client,
		infraProvider,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create GKE provisioner: %w", err)
	}

	return provisioner, gkeConfig, nil
}
