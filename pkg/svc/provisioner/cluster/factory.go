package clusterprovisioner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/devantler-tech/ksail/v6/pkg/apis/cluster/v1alpha1"
	eksctlclient "github.com/devantler-tech/ksail/v6/pkg/client/eksctl"
	k3dconfigmanager "github.com/devantler-tech/ksail/v6/pkg/fsutil/configmanager/k3d"
	kindconfigmanager "github.com/devantler-tech/ksail/v6/pkg/fsutil/configmanager/kind"
	talosconfigmanager "github.com/devantler-tech/ksail/v6/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v6/pkg/svc/detector"
	awsprovider "github.com/devantler-tech/ksail/v6/pkg/svc/provider/aws"
	"github.com/devantler-tech/ksail/v6/pkg/svc/provisioner/cluster/clustererr"
	eksprovisioner "github.com/devantler-tech/ksail/v6/pkg/svc/provisioner/cluster/eks"
	k3dprovisioner "github.com/devantler-tech/ksail/v6/pkg/svc/provisioner/cluster/k3d"
	kindprovisioner "github.com/devantler-tech/ksail/v6/pkg/svc/provisioner/cluster/kind"
	kwokprovisioner "github.com/devantler-tech/ksail/v6/pkg/svc/provisioner/cluster/kwok"
	talosprovisioner "github.com/devantler-tech/ksail/v6/pkg/svc/provisioner/cluster/talos"
	vclusterprovisioner "github.com/devantler-tech/ksail/v6/pkg/svc/provisioner/cluster/vcluster"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/yaml"
)

// Re-export errors for backward compatibility.
var (
	// ErrUnsupportedDistribution is returned when an unsupported distribution is specified.
	ErrUnsupportedDistribution = clustererr.ErrUnsupportedDistribution
	// ErrUnsupportedProvider is returned when an unsupported provider is specified.
	ErrUnsupportedProvider = clustererr.ErrUnsupportedProvider
	// ErrMissingDistributionConfig is returned when no pre-loaded distribution config is provided.
	ErrMissingDistributionConfig = clustererr.ErrMissingDistributionConfig
	// ErrImageVerificationTemplateNotRegularFile is returned when the image verification
	// template path exists but is not a regular file (e.g. it is a directory).
	ErrImageVerificationTemplateNotRegularFile = errors.New(
		"image verification template is not a regular file",
	)
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
	// VCluster holds the pre-loaded vCluster configuration.
	VCluster *VClusterConfig
	// KWOK holds the pre-loaded KWOK configuration.
	KWOK *KWOKConfig
	// EKS holds the pre-loaded EKS configuration.
	EKS *EKSConfig
}

// EKSConfig holds EKS-specific configuration.
type EKSConfig struct {
	// Name is the cluster name (mirrors eksctl.yaml metadata.name).
	Name string
	// Region is the AWS region.
	Region string
	// ConfigPath is the path to the declarative eksctl.yaml.
	ConfigPath string
}

// GetClusterName returns the EKS cluster name.
// This implements the ClusterNameProvider interface used by
// configmanager.GetClusterName.
func (c *EKSConfig) GetClusterName() string {
	return c.Name
}

// KWOKConfig holds KWOK-specific configuration.
type KWOKConfig struct {
	// Name is the cluster name.
	Name string
	// ConfigPath is the optional path to a kwok.yaml configuration file.
	ConfigPath string
}

// GetClusterName returns the KWOK cluster name.
// This implements the ClusterNameProvider interface used by configmanager.GetClusterName.
func (c *KWOKConfig) GetClusterName() string {
	return c.Name
}

// VClusterConfig holds vCluster-specific configuration.
type VClusterConfig struct {
	// Name is the cluster name.
	Name string
	// ValuesPath is the optional path to a vcluster.yaml values file.
	ValuesPath string
	// DisableFlannel disables the built-in flannel CNI in the vCluster.
	// Set to true when a custom CNI (Cilium, Calico) is being installed.
	DisableFlannel bool
}

// GetClusterName returns the vCluster cluster name.
// This implements the ClusterNameProvider interface used by configmanager.GetClusterName.
func (c *VClusterConfig) GetClusterName() string {
	return c.Name
}

// Factory creates distribution-specific cluster provisioners based on the KSail cluster configuration.
type Factory interface {
	Create(ctx context.Context, cluster *v1alpha1.Cluster) (Provisioner, any, error)
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
) (Provisioner, any, error) {
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
	case v1alpha1.DistributionVCluster:
		return f.createVClusterProvisioner(cluster)
	case v1alpha1.DistributionKWOK:
		return f.createKWOKProvisioner(cluster)
	case v1alpha1.DistributionEKS:
		return f.createEKSProvisioner(cluster)
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
) (Provisioner, any, error) {
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

	// Apply containerd image verifier plugin patch when image verification is enabled.
	if cluster.Spec.Cluster.Talos.ImageVerification == v1alpha1.ImageVerificationEnabled {
		kindconfigmanager.ApplyImageVerificationPatches(kindConfig)
	}

	// Apply containerd CDI patch when CDI is enabled.
	cdiVal := cluster.Spec.Cluster.CDI.EffectiveValue(
		cluster.Spec.Cluster.Distribution, cluster.Spec.Cluster.Provider,
	)
	if cdiVal == v1alpha1.CDIEnabled {
		kindconfigmanager.ApplyCDIPatches(kindConfig)
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
) (Provisioner, any, error) {
	if f.DistributionConfig.K3d == nil {
		return nil, nil, fmt.Errorf(
			"k3d config is required for K3d distribution: %w",
			ErrMissingDistributionConfig,
		)
	}

	k3dConfig := f.DistributionConfig.K3d

	// Apply node count overrides from CLI flags (stored in Talos options)
	applyK3dNodeCounts(k3dConfig, cluster.Spec.Cluster.Talos)

	// Apply containerd image verifier plugin volume mount when image verification is enabled.
	// This mounts the generated config.toml.tmpl into K3d node containers so K3s uses it
	// to generate the final containerd config with the image verifier plugin enabled.
	if cluster.Spec.Cluster.Talos.ImageVerification == v1alpha1.ImageVerificationEnabled {
		templatePath := filepath.Join(k3dconfigmanager.DefaultImageVerifierDir, "config.toml.tmpl")

		absTemplatePath, err := filepath.Abs(templatePath)
		if err != nil {
			return nil, nil, fmt.Errorf(
				"failed to resolve k3d image verification template path %q: %w",
				templatePath,
				err,
			)
		}

		fileInfo, err := os.Stat(absTemplatePath)
		if err != nil {
			return nil, nil, fmt.Errorf(
				"k3d image verification template not found at %q; run 'ksail cluster init' to generate it: %w",
				absTemplatePath,
				err,
			)
		}

		if !fileInfo.Mode().IsRegular() {
			return nil, nil, fmt.Errorf(
				"%w: %s; remove it and re-run 'ksail cluster init'",
				ErrImageVerificationTemplateNotRegularFile,
				absTemplatePath,
			)
		}

		k3dconfigmanager.ApplyImageVerificationVolumes(k3dConfig, absTemplatePath)
	}

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
	k3dConfig.Agents = int(opts.Workers)
}

func (f DefaultFactory) createTalosProvisioner(
	cluster *v1alpha1.Cluster,
) (Provisioner, any, error) {
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
		cluster.Spec.Cluster.Connection.Context,
		cluster.Spec.Cluster.Provider,
		cluster.Spec.Cluster.Talos,
		cluster.Spec.Provider.Hetzner,
		cluster.Spec.Provider.Omni,
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

func (f DefaultFactory) createVClusterProvisioner(
	_ *v1alpha1.Cluster,
) (Provisioner, any, error) {
	if f.DistributionConfig.VCluster == nil {
		return nil, nil, fmt.Errorf(
			"vcluster config is required for VCluster distribution: %w",
			ErrMissingDistributionConfig,
		)
	}

	vclusterConfig := f.DistributionConfig.VCluster

	provisioner, err := vclusterprovisioner.CreateProvisioner(
		vclusterConfig.Name,
		vclusterConfig.ValuesPath,
		vclusterConfig.DisableFlannel,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create VCluster provisioner: %w", err)
	}

	return provisioner, vclusterConfig, nil
}

func (f DefaultFactory) createKWOKProvisioner(
	_ *v1alpha1.Cluster,
) (Provisioner, any, error) {
	if f.DistributionConfig.KWOK == nil {
		return nil, nil, fmt.Errorf(
			"kwok config is required for KWOK distribution: %w",
			ErrMissingDistributionConfig,
		)
	}

	kwokConfig := f.DistributionConfig.KWOK

	provisioner := kwokprovisioner.NewProvisioner(
		kwokConfig.Name,
		kwokConfig.ConfigPath,
		nil,
	)

	return provisioner, kwokConfig, nil
}

func (f DefaultFactory) createEKSProvisioner(
	_ *v1alpha1.Cluster,
) (Provisioner, any, error) {
	if f.DistributionConfig.EKS == nil {
		return nil, nil, fmt.Errorf(
			"eks config is required for EKS distribution: %w",
			ErrMissingDistributionConfig,
		)
	}

	eksConfig := f.DistributionConfig.EKS
	client := eksctlclient.NewClient()

	infraProvider, err := awsprovider.NewProvider(client, eksConfig.Region)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create AWS provider: %w", err)
	}

	provisioner, err := eksprovisioner.NewProvisioner(
		eksConfig.Name,
		eksConfig.Region,
		eksConfig.ConfigPath,
		client,
		infraProvider,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create EKS provisioner: %w", err)
	}

	return provisioner, eksConfig, nil
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
