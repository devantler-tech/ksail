package clusterprovisioner

import (
	"context"
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	k3dprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/k3d"
	kindprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/kind"
	talosindockerprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/talosindocker"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// ErrUnsupportedDistribution is returned when an unsupported distribution is specified.
var ErrUnsupportedDistribution = errors.New("unsupported distribution")

// ErrMissingDistributionConfig is returned when no pre-loaded distribution config is provided.
var ErrMissingDistributionConfig = errors.New("missing distribution config")

// DistributionConfig holds pre-loaded distribution-specific configuration.
// This config is used directly by the factory, preserving any in-memory modifications
// (e.g., mirror registries, metrics-server flags).
type DistributionConfig struct {
	// Kind holds the pre-loaded Kind cluster configuration.
	Kind *v1alpha4.Cluster
	// K3d holds the pre-loaded K3d cluster configuration.
	K3d *k3dv1alpha5.SimpleConfig
	// TalosInDocker holds the pre-loaded Talos machine configurations.
	TalosInDocker *talosconfigmanager.Configs
}

// Factory creates distribution-specific cluster provisioners based on the KSail cluster configuration.
type Factory interface {
	Create(ctx context.Context, cluster *v1alpha1.Cluster) (ClusterProvisioner, any, error)
}

// DefaultFactory implements Factory for creating cluster provisioners.
// It requires pre-loaded distribution configs via DistributionConfig to preserve
// any in-memory modifications made before cluster creation.
type DefaultFactory struct {
	// DistributionConfig holds pre-loaded distribution-specific configuration.
	// This is required and must contain the appropriate config for the cluster's distribution.
	DistributionConfig *DistributionConfig
}

// Create selects the correct distribution provisioner for the KSail cluster configuration.
// It requires DistributionConfig to be set with the appropriate pre-loaded config.
func (f DefaultFactory) Create(
	_ context.Context,
	cluster *v1alpha1.Cluster,
) (ClusterProvisioner, any, error) {
	if cluster == nil {
		return nil, nil, fmt.Errorf(
			"cluster configuration is required: %w",
			ErrUnsupportedDistribution,
		)
	}

	if f.DistributionConfig == nil {
		return nil, nil, fmt.Errorf(
			"distribution config is required: %w",
			ErrMissingDistributionConfig,
		)
	}

	switch cluster.Spec.Cluster.Distribution {
	case v1alpha1.DistributionKind:
		return f.createKindProvisioner(cluster)
	case v1alpha1.DistributionK3d:
		return f.createK3dProvisioner(cluster)
	case v1alpha1.DistributionTalosInDocker:
		return f.createTalosInDockerProvisioner(cluster)
	default:
		return nil, "", fmt.Errorf(
			"%w: %s",
			ErrUnsupportedDistribution,
			cluster.Spec.Cluster.Distribution,
		)
	}
}

func (f DefaultFactory) createKindProvisioner(
	cluster *v1alpha1.Cluster,
) (ClusterProvisioner, any, error) {
	if f.DistributionConfig.Kind == nil {
		return nil, nil, fmt.Errorf(
			"kind config is required for Kind distribution: %w",
			ErrMissingDistributionConfig,
		)
	}

	kindConfig := f.DistributionConfig.Kind

	// Apply node count overrides from CLI flags (stored in TalosInDocker options)
	applyKindNodeCounts(kindConfig, cluster.Spec.Cluster.Options.TalosInDocker)

	provisioner, err := kindprovisioner.CreateProvisioner(
		kindConfig,
		cluster.Spec.Cluster.Connection.Kubeconfig,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Kind provisioner: %w", err)
	}

	return provisioner, kindConfig, nil
}

// applyKindNodeCounts applies node count overrides from CLI flags to the Kind config.
// This enables --control-planes and --workers CLI flags to override the kind.yaml at runtime.
func applyKindNodeCounts(kindConfig *v1alpha4.Cluster, opts v1alpha1.OptionsTalosInDocker) {
	// Only apply if explicitly set (non-zero values indicate override)
	if opts.ControlPlanes <= 0 && opts.Workers <= 0 {
		return
	}

	// Calculate target node counts
	targetCP := int(opts.ControlPlanes)
	if targetCP <= 0 {
		targetCP = 1 // default to 1 control-plane
	}
	targetWorkers := int(opts.Workers)

	// Build new nodes slice based on target counts
	var newNodes []v1alpha4.Node

	// Add control-plane nodes
	for range targetCP {
		newNodes = append(newNodes, v1alpha4.Node{Role: v1alpha4.ControlPlaneRole})
	}

	// Add worker nodes
	for range targetWorkers {
		newNodes = append(newNodes, v1alpha4.Node{Role: v1alpha4.WorkerRole})
	}

	kindConfig.Nodes = newNodes
}

func (f DefaultFactory) createK3dProvisioner(
	cluster *v1alpha1.Cluster,
) (ClusterProvisioner, any, error) {
	if f.DistributionConfig.K3d == nil {
		return nil, nil, fmt.Errorf(
			"K3d config is required for K3d distribution: %w",
			ErrMissingDistributionConfig,
		)
	}

	k3dConfig := f.DistributionConfig.K3d

	// Apply node count overrides from CLI flags (stored in TalosInDocker options)
	applyK3dNodeCounts(k3dConfig, cluster.Spec.Cluster.Options.TalosInDocker)

	provisioner := k3dprovisioner.CreateProvisioner(
		k3dConfig,
		cluster.Spec.Cluster.DistributionConfig,
	)

	return provisioner, k3dConfig, nil
}

// applyK3dNodeCounts applies node count overrides from CLI flags to the K3d config.
// This enables --control-planes and --workers CLI flags to override the k3d.yaml at runtime.
func applyK3dNodeCounts(k3dConfig *k3dv1alpha5.SimpleConfig, opts v1alpha1.OptionsTalosInDocker) {
	// Only apply if explicitly set (non-zero values indicate override)
	if opts.ControlPlanes <= 0 && opts.Workers <= 0 {
		return
	}

	// Apply server (control-plane) count if explicitly set
	if opts.ControlPlanes > 0 {
		k3dConfig.Servers = int(opts.ControlPlanes)
	}

	// Apply agent (worker) count - 0 is valid when control-planes is set
	if opts.ControlPlanes > 0 {
		k3dConfig.Agents = int(opts.Workers)
	} else if opts.Workers > 0 {
		k3dConfig.Agents = int(opts.Workers)
	}
}

func (f DefaultFactory) createTalosInDockerProvisioner(
	cluster *v1alpha1.Cluster,
) (ClusterProvisioner, any, error) {
	if f.DistributionConfig.TalosInDocker == nil {
		return nil, nil, fmt.Errorf(
			"TalosInDocker config is required for TalosInDocker distribution: %w",
			ErrMissingDistributionConfig,
		)
	}

	provisioner, err := talosindockerprovisioner.CreateProvisioner(
		f.DistributionConfig.TalosInDocker,
		cluster.Spec.Cluster.Connection.Kubeconfig,
		cluster.Spec.Cluster.Options.TalosInDocker,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create TalosInDocker provisioner: %w", err)
	}

	return provisioner, f.DistributionConfig.TalosInDocker, nil
}
