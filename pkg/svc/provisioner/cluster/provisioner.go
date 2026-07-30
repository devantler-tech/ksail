package clusterprovisioner

import (
	"context"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/detector"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
)

// Provisioner defines methods for managing Kubernetes clusters.
// Provisioners handle distribution-specific operations (bootstrapping, configuration)
// while delegating infrastructure operations to a Provider.
type Provisioner interface {
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

// Updater is an optional interface for provisioners that support in-place updates.
// Not all provisioners support updates - Kind requires recreation for most changes,
// while Talos and K3d support various in-place modifications.
type Updater interface {
	// Update applies configuration changes to a running cluster.
	// Returns an UpdateResult describing what changed and how it was handled.
	// The oldSpec represents the current cluster state, newSpec is the desired state.
	Update(
		ctx context.Context,
		name string,
		oldSpec, newSpec *v1alpha1.ClusterSpec,
		opts clusterupdate.UpdateOptions,
	) (*clusterupdate.UpdateResult, error)

	// DiffConfig computes the differences between current and desired configurations.
	// This is used to preview changes before applying them.
	DiffConfig(
		ctx context.Context,
		name string,
		oldSpec, newSpec *v1alpha1.ClusterSpec,
	) (*clusterupdate.UpdateResult, error)

	// GetCurrentConfig retrieves the current cluster configuration from the running cluster.
	// Used to compare against the desired configuration for computing diffs.
	// The clusterName parameter is used to merge persisted state for fields that cannot
	// be introspected from the live cluster (e.g., Talos ISO, local registry settings,
	// containerd mirrors directory) via clusterupdate.MergePersistedState — Kind, K3d, and
	// Talos all do this so a configured value never reads as a false recreate-required diff.
	// Returns the cluster spec and an optional provider spec (non-nil for provider-aware provisioners).
	GetCurrentConfig(
		ctx context.Context,
		clusterName string,
	) (*v1alpha1.ClusterSpec, *v1alpha1.ProviderSpec, error)
}

// InPlaceFieldSupport is an optional capability for updaters that implement a
// deliberately narrow subset of the spec-level in-place diff vocabulary. The
// orchestrator promotes any unhandled field to recreate-required rather than
// recording an unapplied desired value as the new baseline.
type InPlaceFieldSupport interface {
	SupportsInPlaceField(field string) bool
}

// ProviderAware is an optional interface for provisioners that can use a provider
// for infrastructure operations (start/stop nodes).
type ProviderAware interface {
	// SetProvider sets the infrastructure provider for node operations.
	SetProvider(p provider.Provider)
}

// Connector is an optional capability for provisioners whose provisioned (child) cluster is
// reachable from where the operator runs. The operator uses Kubeconfig to obtain credentials for
// installing components into the child cluster. Provisioners that cannot expose the child to the
// operator (e.g. a kubeconfig bound to the Docker network, unreachable from a hub pod) omit it, and
// the operator skips component installation for those clusters.
type Connector interface {
	// Kubeconfig returns a kubeconfig for the named provisioned cluster with its API server address
	// rewritten to one reachable from where the operator runs (e.g. an in-cluster Service address),
	// not necessarily from a CLI host. It returns clustererr.ErrKubeconfigNotReady when the child
	// credentials have not been published yet, so the caller can retry.
	Kubeconfig(ctx context.Context, name string) ([]byte, error)
}

// KubeconfigRefresher is an optional interface for provisioners that can
// refresh the kubeconfig for a running cluster from a remote source.
// This is needed for remote providers (e.g., Omni) where the kubeconfig
// is not persisted locally and must be fetched from the provider API.
type KubeconfigRefresher interface {
	// RefreshKubeconfig fetches and saves the kubeconfig for the named cluster.
	RefreshKubeconfig(ctx context.Context, name string) error
}

// ComponentDetectorAware is an optional interface for provisioners that
// accept a component detector for probing installed cluster components.
type ComponentDetectorAware interface {
	// SetComponentDetector sets the component detector used by GetCurrentConfig
	// to return accurate live state instead of static defaults.
	SetComponentDetector(d *detector.ComponentDetector)
}
