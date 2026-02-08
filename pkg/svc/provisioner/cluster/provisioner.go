package clusterprovisioner

import (
	"context"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/types"
)

// ClusterProvisioner defines methods for managing Kubernetes clusters.
// Provisioners handle distribution-specific operations (bootstrapping, configuration)
// while delegating infrastructure operations to a Provider.
type ClusterProvisioner interface {
	// Create creates a Kubernetes cluster. If name is non-empty, target that name; otherwise use config defaults.
	Create(ctx context.Context, name string) error

	// Delete deletes a Kubernetes cluster by name or config default when name is empty.
	Delete(ctx context.Context, name string) error

	// Start starts a Kubernetes cluster by name or config default when name is empty.
	Start(ctx context.Context, name string) error

	// Stop stops a Kubernetes cluster by name or config default when name is empty.
	Stop(ctx context.Context, name string) error

	// List lists all Kubernetes clusters.
	List(ctx context.Context) ([]string, error)

	// Exists checks if a Kubernetes cluster exists by name or config default when name is empty.
	Exists(ctx context.Context, name string) (bool, error)
}

// ClusterUpdater is an optional interface for provisioners that support in-place updates.
// Not all provisioners support updates - Kind requires recreation for most changes,
// while Talos and K3d support various in-place modifications.
type ClusterUpdater interface {
	// Update applies configuration changes to a running cluster.
	// Returns an UpdateResult describing what changed and how it was handled.
	// The oldSpec represents the current cluster state, newSpec is the desired state.
	Update(
		ctx context.Context,
		name string,
		oldSpec, newSpec *v1alpha1.ClusterSpec,
		opts types.UpdateOptions,
	) (*types.UpdateResult, error)

	// DiffConfig computes the differences between current and desired configurations.
	// This is used to preview changes before applying them.
	DiffConfig(
		ctx context.Context,
		name string,
		oldSpec, newSpec *v1alpha1.ClusterSpec,
	) (*types.UpdateResult, error)

	// GetCurrentConfig retrieves the current cluster configuration from the running cluster.
	// Used to compare against the desired configuration for computing diffs.
	GetCurrentConfig(ctx context.Context) (*v1alpha1.ClusterSpec, error)
}

// ProviderAware is an optional interface for provisioners that can use a provider
// for infrastructure operations (start/stop nodes).
type ProviderAware interface {
	// SetProvider sets the infrastructure provider for node operations.
	SetProvider(p provider.Provider)
}
