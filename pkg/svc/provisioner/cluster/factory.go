package clusterprovisioner

import (
	"context"
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	kindconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/kind"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	"github.com/devantler-tech/ksail/v5/pkg/svc/detector"
	clustererrors "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/errors"
	k3dprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/k3d"
	kindprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/kind"
	talosprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/talos"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/yaml"
)

// Re-export errors for backward compatibility.
var (
	// ErrUnsupportedDistribution is returned when an unsupported distribution is specified.
	ErrUnsupportedDistribution = clustererrors.ErrUnsupportedDistribution
	// ErrUnsupportedProvider is returned when an unsupported provider is specified.
	ErrUnsupportedProvider = clustererrors.ErrUnsupportedProvider
	// ErrMissingDistributionConfig is returned when no pre-loaded distribution config is provided.
	ErrMissingDistributionConfig = clustererrors.ErrMissingDistributionConfig
)

// DistributionConfig holds pre-loaded distribution-specific configuration.
// This config is used directly by the factory, preserving any in-memory modifications
// (e.g., mirror registries, metrics-server flags).
type DistributionConfig struct {
	// Kind holds the pre-loaded Kind cluster configuration.
	Kind *v1alpha4.Cluster
	// K3d holds the pre-loaded K3d cluster configuration.
	K3d *k3dv1alpha5.SimpleConfig
	// Talos holds the pre-loaded Talos machine configurations.
	Talos *talosconfigmanager.Configs
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

	// ComponentDetector is an optional detector used to probe running clusters
	// for installed components. When non-nil it is injected into provisioners
	// so that GetCurrentConfig returns accurate live state instead of defaults.
	ComponentDetector *detector.ComponentDetector
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
	case v1alpha1.DistributionVanilla:
		return f.createKindProvisioner(cluster)
	case v1alpha1.DistributionK3s:
		return f.createK3dProvisioner(cluster)
	case v1alpha1.DistributionTalos:
		return f.createTalosProvisioner(cluster)
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
			"kind config is required for Vanilla distribution: %w",
			ErrMissingDistributionConfig,
		)
	}

	kindConfig := f.DistributionConfig.Kind

	// Apply node count overrides from CLI flags (stored in Talos options)
	applyKindNodeCounts(kindConfig, cluster.Spec.Cluster.Talos)

	// Apply kubelet certificate rotation patches when metrics-server is enabled.
	// This must happen AFTER applyKindNodeCounts since that function may replace the nodes slice.
	if cluster.Spec.Cluster.MetricsServer == v1alpha1.MetricsServerEnabled {
		kindconfigmanager.ApplyKubeletCertRotationPatches(kindConfig)
	}

	provisioner, err := kindprovisioner.CreateProvisioner(
		kindConfig,
		cluster.Spec.Cluster.Connection.Kubeconfig,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Kind provisioner: %w", err)
	}

	if f.ComponentDetector != nil {
		provisioner.WithComponentDetector(f.ComponentDetector)
	}

	return provisioner, kindConfig, nil
}

// applyKindNodeCounts applies node count overrides from CLI flags to the Kind config.
// This enables --control-planes and --workers CLI flags to override the kind.yaml at runtime.
func applyKindNodeCounts(kindConfig *v1alpha4.Cluster, opts v1alpha1.OptionsTalos) {
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
	newNodes := make([]v1alpha4.Node, 0, targetCP+targetWorkers)

	// Add control-plane nodes with default image
	for range targetCP {
		newNodes = append(newNodes, v1alpha4.Node{
			Role:  v1alpha4.ControlPlaneRole,
			Image: kindconfigmanager.DefaultKindNodeImage,
		})
	}

	// Add worker nodes with default image
	for range targetWorkers {
		newNodes = append(newNodes, v1alpha4.Node{
			Role:  v1alpha4.WorkerRole,
			Image: kindconfigmanager.DefaultKindNodeImage,
		})
	}

	kindConfig.Nodes = newNodes
}

func (f DefaultFactory) createK3dProvisioner(
	cluster *v1alpha1.Cluster,
) (ClusterProvisioner, any, error) {
	if f.DistributionConfig.K3d == nil {
		return nil, nil, fmt.Errorf(
			"k3d config is required for K3d distribution: %w",
			ErrMissingDistributionConfig,
		)
	}

	k3dConfig := f.DistributionConfig.K3d

	// Apply node count overrides from CLI flags (stored in Talos options)
	applyK3dNodeCounts(k3dConfig, cluster.Spec.Cluster.Talos)

	// Write the in-memory config to a temp file so k3d picks up any modifications
	// (e.g., registry mirrors configured via --mirror-registry, node counts).
	// We always use a temp file to avoid modifying the user's k3d.yaml.
	// The k3d CLI reads configuration from file, not from our in-memory config.
	tempConfigPath, err := writeK3dConfigToTempFile(k3dConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to write k3d config to temp file: %w", err)
	}

	provisioner := k3dprovisioner.CreateProvisioner(
		k3dConfig,
		tempConfigPath,
	)

	if f.ComponentDetector != nil {
		provisioner.WithComponentDetector(f.ComponentDetector)
	}

	return provisioner, k3dConfig, nil
}

// applyK3dNodeCounts applies node count overrides from CLI flags to the K3d config.
// This enables --control-planes and --workers CLI flags to override the k3d.yaml at runtime.
func applyK3dNodeCounts(k3dConfig *k3dv1alpha5.SimpleConfig, opts v1alpha1.OptionsTalos) {
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

func (f DefaultFactory) createTalosProvisioner(
	cluster *v1alpha1.Cluster,
) (ClusterProvisioner, any, error) {
	if f.DistributionConfig.Talos == nil {
		return nil, nil, fmt.Errorf(
			"talos config is required for Talos distribution: %w",
			ErrMissingDistributionConfig,
		)
	}

	// Always skip CNI-dependent checks (CoreDNS, kube-proxy) for Talos Docker provisioner.
	//
	// Rationale:
	// 1. Custom CNI (Cilium, Calico): Pods cannot start until CNI is installed post-bootstrap.
	// 2. Default Flannel CNI: While Flannel is bundled with Talos, it can be slow or unreliable
	//    in containerized environments (GitHub Actions, Docker-in-Docker). The checks for
	//    kube-proxy and CoreDNS can timeout even when the cluster is fundamentally healthy.
	//
	// Since we've verified that etcd, kubelet, and the Kubernetes API are healthy via
	// PreBootSequenceChecks, the cluster is functional. Application-level DNS/proxy
	// services will become ready shortly after bootstrap completes.
	skipCNIChecks := true

	provisioner, err := talosprovisioner.CreateProvisioner(
		f.DistributionConfig.Talos,
		cluster.Spec.Cluster.Connection.Kubeconfig,
		cluster.Spec.Cluster.Provider,
		cluster.Spec.Cluster.Talos,
		cluster.Spec.Cluster.Hetzner,
		skipCNIChecks,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Talos provisioner: %w", err)
	}

	if f.ComponentDetector != nil {
		provisioner.WithComponentDetector(f.ComponentDetector)
	}

	return provisioner, f.DistributionConfig.Talos, nil
}

// writeK3dConfigToTempFile writes the in-memory k3d config to a temporary file.
// This approach avoids modifying the user's k3d.yaml while ensuring k3d picks up
// all in-memory modifications (registry mirrors, node counts, etc.).
// The temp file persists until system cleanup - this is intentional since k3d
// may reference the config path during cluster operations.
func writeK3dConfigToTempFile(config *k3dv1alpha5.SimpleConfig) (string, error) {
	data, err := yaml.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("marshal k3d config: %w", err)
	}

	// Create temp file with k3d prefix for easy identification
	tempFile, err := os.CreateTemp("", "ksail-k3d-*.yaml")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}

	filePath := tempFile.Name()

	_, writeErr := tempFile.Write(data)

	closeErr := tempFile.Close()

	if writeErr != nil {
		return "", fmt.Errorf("write to temp file: %w", writeErr)
	}

	if closeErr != nil {
		return "", fmt.Errorf("close temp file: %w", closeErr)
	}

	return filePath, nil
}
