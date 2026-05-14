package clusterprovisioner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	eksctlclient "github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	k3dconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/k3d"
	kindconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/kind"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/svc/detector"
	awsprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/aws"
	kubernetesprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	eksprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/eks"
	k3dprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/k3d"
	kindprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kind"
	kwokprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kwok"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	vclusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/vcluster"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
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

	// Apply node count overrides from CLI flags / cluster-level config.
	applyKindNodeCounts(
		kindConfig,
		cluster.Spec.Cluster.ControlPlanes,
		cluster.Spec.Cluster.Workers,
	)

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

	// Kubernetes provider: run Kind inside a DinD pod on the host cluster
	if cluster.Spec.Cluster.Provider == v1alpha1.ProviderKubernetes {
		return f.createKindKubernetesProvisioner(cluster, kindConfig)
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

// createKindKubernetesProvisioner creates a Kind provisioner that runs inside
// a DinD pod on a host Kubernetes cluster.
func (f DefaultFactory) createKindKubernetesProvisioner(
	cluster *v1alpha1.Cluster,
	kindConfig *v1alpha4.Cluster,
) (Provisioner, any, error) {
	opts := cluster.Spec.Provider.Kubernetes

	// Use kindConfig.Name — it's set by applyClusterNameOverride,
	// while cluster.Metadata.Name may be empty with --name flag.
	clusterName := kindConfig.Name

	_, restConfig, dynClient, k8sProvider, err := buildKubernetesInfra(opts)
	if err != nil {
		return nil, nil, err
	}

	// Configure Kind for DinD: API server must bind to all interfaces
	// and use a fixed port so it's accessible from outside the DinD container.
	applyKindDinDNetworking(kindConfig)

	provisioner, err := kindprovisioner.NewKubernetesProvisioner(
		kindprovisioner.KubernetesProvisionerConfig{
			KindConfig:       kindConfig,
			KubeconfigPath:   cluster.Spec.Cluster.Connection.Kubeconfig,
			K8sProvider:      k8sProvider,
			DynamicClient:    dynClient,
			RestConfig:       restConfig,
			ClusterName:      clusterName,
			Distribution:     string(cluster.Spec.Cluster.Distribution),
			GatewayClassName: opts.GatewayClassName,
			APIServerPort:    kubernetesprovider.DinDAPIServerPort,
		},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("create Kind Kubernetes provisioner: %w", err)
	}

	if f.ComponentDetector != nil {
		provisioner.WithComponentDetector(f.ComponentDetector)
	}

	return provisioner, kindConfig, nil
}

// applyKindDinDNetworking configures Kind networking for DinD execution.
// The API server must bind to 0.0.0.0 (all interfaces) so it's accessible
// from outside the DinD container. A fixed port (6443) is used instead of
// a random port to enable deterministic port mapping. The certSANs patch
// ensures the API server TLS certificate includes 127.0.0.1 and localhost,
// which are needed when accessing the API via port-forward.
func applyKindDinDNetworking(kindConfig *v1alpha4.Cluster) {
	kindConfig.Networking.APIServerAddress = "0.0.0.0"
	kindConfig.Networking.APIServerPort = kubernetesprovider.DinDAPIServerPort

	// Add certSANs to all control-plane nodes so the API server cert
	// is valid for 127.0.0.1 (port-forward) and localhost.
	certSANsPatch := `kind: ClusterConfiguration
apiServer:
  certSANs:
  - "127.0.0.1"
  - "localhost"
  - "0.0.0.0"`

	for i, node := range kindConfig.Nodes {
		if node.Role == v1alpha4.ControlPlaneRole {
			kindConfig.Nodes[i].KubeadmConfigPatches = append(
				kindConfig.Nodes[i].KubeadmConfigPatches,
				certSANsPatch,
			)
		}
	}
	// If no nodes are defined, add one with the patch
	if len(kindConfig.Nodes) == 0 {
		kindConfig.Nodes = []v1alpha4.Node{
			{
				Role:                 v1alpha4.ControlPlaneRole,
				KubeadmConfigPatches: []string{certSANsPatch},
			},
		}
	}
}

// createK3dKubernetesProvisioner creates a K3s provisioner that runs inside
// a host Kubernetes cluster using the k3k operator.
func (f DefaultFactory) createK3dKubernetesProvisioner(
	cluster *v1alpha1.Cluster,
) (Provisioner, any, error) {
	opts := cluster.Spec.Provider.Kubernetes

	// resolveClusterNameFromContext normally extracts from k3dConfig.Name,
	// but k3d config is skipped for the Kubernetes provider path.
	// Derive from connection context (set by applyClusterNameOverride),
	// stripping the "k3d-" prefix that ContextName() adds.
	clusterName := strings.TrimPrefix(
		cluster.Spec.Cluster.Connection.Context, "k3d-",
	)
	if clusterName == "" {
		clusterName = cluster.Metadata.Name
	}

	hostClient, restConfig, _, k8sProvider, err := buildKubernetesInfra(opts)
	if err != nil {
		return nil, nil, err
	}

	controlPlanes := cluster.Spec.Cluster.ControlPlanes
	if controlPlanes <= 0 {
		controlPlanes = 1
	}

	workers := cluster.Spec.Cluster.Workers

	provisioner := k3dprovisioner.NewK3kProvisioner(
		k3dprovisioner.K3kProvisionerConfig{
			HostClientset:  hostClient,
			RestConfig:     restConfig,
			K8sProvider:    k8sProvider,
			ClusterName:    clusterName,
			KubeconfigPath: cluster.Spec.Cluster.Connection.Kubeconfig,
			HostContext:    resolveKubernetesOption(opts.Context, opts.ContextEnvVar),
			ControlPlanes:  controlPlanes,
			Workers:        workers,
			PodCIDR:        opts.PodCIDR,
			ServiceCIDR:    opts.ServiceCIDR,
		},
	)

	if f.ComponentDetector != nil {
		provisioner.WithComponentDetector(f.ComponentDetector)
	}

	return provisioner, nil, nil
}

// buildHostClusterClients builds Kubernetes clients for the host cluster
// from the Kubernetes provider options.
func buildHostClusterClients(
	opts v1alpha1.OptionsKubernetes,
) (kubernetes.Interface, *rest.Config, dynamic.Interface, error) {
	kubeconfig := resolveKubernetesOption(opts.Kubeconfig, opts.KubeconfigEnvVar)

	// Fall back to default kubeconfig path if not set
	if kubeconfig == "" {
		kubeconfig = k8s.DefaultKubeconfigPath()
	}

	// Canonicalize the path (expand ~, resolve symlinks)
	kubeconfig, err := fsutil.ExpandHomePath(kubeconfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("expand kubeconfig path: %w", err)
	}

	kubeconfig, err = fsutil.EvalCanonicalPath(kubeconfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("canonicalize kubeconfig path: %w", err)
	}

	context := resolveKubernetesOption(opts.Context, opts.ContextEnvVar)

	restConfig, err := k8s.BuildRESTConfig(kubeconfig, context)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build host REST config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create host clientset: %w", err)
	}

	dynClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create host dynamic client: %w", err)
	}

	return clientset, restConfig, dynClient, nil
}

// resolveKubernetesOption returns the value from the environment variable
// if set, otherwise returns the direct value.
func resolveKubernetesOption(directValue, envVar string) string {
	if envVar != "" {
		if envValue := os.Getenv(envVar); envValue != "" {
			return envValue
		}
	}

	return directValue
}

// buildKubernetesInfra creates the host-cluster clients and Kubernetes provider in one call.
// This consolidates the repeated buildHostClusterClients + NewProvider sequence.
func buildKubernetesInfra(
	opts v1alpha1.OptionsKubernetes,
) (kubernetes.Interface, *rest.Config, dynamic.Interface, *kubernetesprovider.Provider, error) {
	hostClient, restConfig, dynClient, err := buildHostClusterClients(opts)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("build host cluster clients: %w", err)
	}

	k8sProvider, err := kubernetesprovider.NewProvider(hostClient, opts)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("create Kubernetes provider: %w", err)
	}

	return hostClient, restConfig, dynClient, k8sProvider, nil
}

// applyKindNodeCounts applies node count overrides from CLI flags / cluster-level
// config to the Kind config. Enables --control-planes and --workers CLI flags to
// override kind.yaml at runtime.
func applyKindNodeCounts(kindConfig *v1alpha4.Cluster, controlPlanes, workers int32) {
	if controlPlanes <= 0 && workers <= 0 {
		return
	}

	targetCP := int(controlPlanes)
	if targetCP <= 0 {
		targetCP = 1 // default to 1 control-plane
	}

	targetWorkers := int(workers)

	newNodes := make([]v1alpha4.Node, 0, targetCP+targetWorkers)

	for range targetCP {
		newNodes = append(newNodes, v1alpha4.Node{
			Role:  v1alpha4.ControlPlaneRole,
			Image: kindconfigmanager.DefaultKindNodeImage,
		})
	}

	for range targetWorkers {
		newNodes = append(newNodes, v1alpha4.Node{
			Role:  v1alpha4.WorkerRole,
			Image: kindconfigmanager.DefaultKindNodeImage,
		})
	}

	kindConfig.Nodes = newNodes
}

func (f DefaultFactory) createK3dProvisioner( //nolint:funlen // sequential setup steps
	cluster *v1alpha1.Cluster,
) (Provisioner, any, error) {
	// Kubernetes provider: use k3k operator instead of Docker-based K3d
	if cluster.Spec.Cluster.Provider == v1alpha1.ProviderKubernetes {
		return f.createK3dKubernetesProvisioner(cluster)
	}

	if f.DistributionConfig.K3d == nil {
		return nil, nil, fmt.Errorf(
			"k3d config is required for K3d distribution: %w",
			ErrMissingDistributionConfig,
		)
	}

	k3dConfig := f.DistributionConfig.K3d

	// Apply node count overrides from CLI flags / cluster-level config.
	applyK3dNodeCounts(k3dConfig, cluster.Spec.Cluster.ControlPlanes, cluster.Spec.Cluster.Workers)

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

// applyK3dNodeCounts applies node count overrides from CLI flags / cluster-level
// config to the K3d config. Enables --control-planes and --workers CLI flags to
// override k3d.yaml at runtime.
func applyK3dNodeCounts(k3dConfig *k3dv1alpha5.SimpleConfig, controlPlanes, workers int32) {
	if controlPlanes <= 0 && workers <= 0 {
		return
	}

	if controlPlanes > 0 {
		k3dConfig.Servers = int(controlPlanes)
	}

	k3dConfig.Agents = int(workers)
}

func (f DefaultFactory) createTalosProvisioner(
	cluster *v1alpha1.Cluster,
) (Provisioner, any, error) {
	// Kubernetes provider: run Talos inside a DinD pod on the host cluster
	if cluster.Spec.Cluster.Provider == v1alpha1.ProviderKubernetes {
		return f.createTalosKubernetesProvisioner(cluster)
	}

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

	// Overlay cluster-level node counts onto Talos options for downstream consumers.
	// Bridges the deprecated Talos-scoped fields during the migration window.
	talosOpts := cluster.Spec.Cluster.Talos
	//nolint:staticcheck // intentional: bridging deprecated field
	talosOpts.ControlPlanes = cluster.Spec.Cluster.ControlPlanes
	//nolint:staticcheck // intentional: bridging deprecated field
	talosOpts.Workers = cluster.Spec.Cluster.Workers

	// Propagate autoscaler-enabled flag to Hetzner options so the provisioner
	// can create the cluster-autoscaler-config Secret during bootstrap.
	hetznerOpts := cluster.Spec.Provider.Hetzner

	hetznerOpts.NodeAutoscalerEnabled = cluster.Spec.Cluster.Autoscaler.Node.Enabled ||
		cluster.Spec.Cluster.NodeAutoscaling == v1alpha1.NodeAutoscalingEnabled

	// Derive pool names from the new autoscaler pools config so that the
	// delete path can clean up autoscaler-managed Hetzner servers.
	if len(hetznerOpts.AutoscalerNodePoolNames) == 0 {
		pools := cluster.Spec.Cluster.Autoscaler.Node.Pools
		if len(pools) > 0 {
			names := make([]string, len(pools))
			for i, pool := range pools {
				names[i] = pool.Name
			}

			hetznerOpts.AutoscalerNodePoolNames = names
		}
	}

	provisioner, err := talosprovisioner.CreateProvisioner(
		f.DistributionConfig.Talos,
		cluster.Spec.Cluster.Connection.Kubeconfig,
		cluster.Spec.Cluster.Connection.Context,
		cluster.Spec.Cluster.Provider,
		talosOpts,
		hetznerOpts,
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

// createTalosKubernetesProvisioner creates a Talos provisioner that runs inside
// a DinD pod on a host Kubernetes cluster via the Talos SDK.
func (f DefaultFactory) createTalosKubernetesProvisioner(
	cluster *v1alpha1.Cluster,
) (Provisioner, any, error) {
	if f.DistributionConfig.Talos == nil {
		return nil, nil, fmt.Errorf(
			"talos config is required for Talos distribution: %w",
			ErrMissingDistributionConfig,
		)
	}

	opts := cluster.Spec.Provider.Kubernetes

	// Derive cluster name from Talos config (set by applyClusterNameOverride).
	clusterName := f.DistributionConfig.Talos.GetClusterName()
	if clusterName == "" {
		clusterName = cluster.Metadata.Name
	}

	_, restConfig, dynClient, k8sProvider, err := buildKubernetesInfra(opts)
	if err != nil {
		return nil, nil, err
	}

	// Create a full inner Talos Provisioner (Docker provider type).
	// The Docker client will be injected at Create() time after DinD is ready.
	talosOpts := cluster.Spec.Cluster.Talos
	//nolint:staticcheck // intentional: bridging deprecated field
	talosOpts.ControlPlanes = cluster.Spec.Cluster.ControlPlanes
	//nolint:staticcheck // intentional: bridging deprecated field
	talosOpts.Workers = cluster.Spec.Cluster.Workers

	innerProvisioner, err := talosprovisioner.CreateProvisioner(
		f.DistributionConfig.Talos,
		cluster.Spec.Cluster.Connection.Kubeconfig,
		"",
		v1alpha1.ProviderDocker,
		talosOpts,
		v1alpha1.OptionsHetzner{},
		v1alpha1.OptionsOmni{},
		true, // skipCNIChecks — same as normal Docker path
	)
	if err != nil {
		return nil, nil, fmt.Errorf("create inner Talos provisioner: %w", err)
	}

	provisioner := talosprovisioner.NewKubernetesProvisioner(
		talosprovisioner.KubernetesProvisionerConfig{
			InnerProvisioner: innerProvisioner,
			KubeconfigPath:   cluster.Spec.Cluster.Connection.Kubeconfig,
			K8sProvider:      k8sProvider,
			DynamicClient:    dynClient,
			RestConfig:       restConfig,
			ClusterName:      clusterName,
			Distribution:     string(cluster.Spec.Cluster.Distribution),
			GatewayClassName: opts.GatewayClassName,
			ControlPlanes:    int(cluster.Spec.Cluster.ControlPlanes),
			Workers:          int(cluster.Spec.Cluster.Workers),
		},
	)

	return provisioner, nil, nil
}

func (f DefaultFactory) createVClusterProvisioner(
	cluster *v1alpha1.Cluster,
) (Provisioner, any, error) {
	if cluster.Spec.Cluster.Provider == v1alpha1.ProviderKubernetes {
		return f.createVClusterKubernetesProvisioner(cluster)
	}

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

// createVClusterKubernetesProvisioner creates a vCluster provisioner that
// deploys vCluster as a Helm release on a host Kubernetes cluster.
func (f DefaultFactory) createVClusterKubernetesProvisioner(
	cluster *v1alpha1.Cluster,
) (Provisioner, any, error) {
	opts := cluster.Spec.Provider.Kubernetes

	// Use VCluster config name (set by applyClusterNameOverride).
	vclusterConfig := f.DistributionConfig.VCluster
	clusterName := ""
	if vclusterConfig != nil {
		clusterName = vclusterConfig.Name
	}

	if clusterName == "" {
		clusterName = cluster.Metadata.Name
	}

	hostClient, restConfig, _, k8sProvider, err := buildKubernetesInfra(opts)
	if err != nil {
		return nil, nil, err
	}

	var valuesPath string
	var disableFlannel bool
	if vclusterConfig != nil {
		valuesPath = vclusterConfig.ValuesPath
		disableFlannel = vclusterConfig.DisableFlannel
	}

	provisioner := vclusterprovisioner.NewKubernetesProvisioner(
		vclusterprovisioner.KubernetesProvisionerConfig{
			ClusterName:    clusterName,
			HostContext:    resolveKubernetesOption(opts.Context, opts.ContextEnvVar),
			KubeconfigPath: cluster.Spec.Cluster.Connection.Kubeconfig,
			HostClientset:  hostClient,
			RestConfig:     restConfig,
			K8sProvider:    k8sProvider,
			ValuesPath:     valuesPath,
			DisableFlannel: disableFlannel,
		},
	)

	return provisioner, vclusterConfig, nil
}

func (f DefaultFactory) createKWOKProvisioner(
	cluster *v1alpha1.Cluster,
) (Provisioner, any, error) {
	if f.DistributionConfig.KWOK == nil {
		return nil, nil, fmt.Errorf(
			"kwok config is required for KWOK distribution: %w",
			ErrMissingDistributionConfig,
		)
	}

	kwokConfig := f.DistributionConfig.KWOK

	// Kubernetes provider: run KWOK inside a DinD pod on the host cluster
	if cluster.Spec.Cluster.Provider == v1alpha1.ProviderKubernetes {
		return f.createKWOKKubernetesProvisioner(cluster, kwokConfig)
	}

	provisioner := kwokprovisioner.NewProvisioner(
		kwokConfig.Name,
		kwokConfig.ConfigPath,
		nil,
	)

	return provisioner, kwokConfig, nil
}

// createKWOKKubernetesProvisioner creates a KWOK provisioner that runs inside
// a DinD pod on a host Kubernetes cluster.
func (f DefaultFactory) createKWOKKubernetesProvisioner(
	cluster *v1alpha1.Cluster,
	kwokConfig *KWOKConfig,
) (Provisioner, any, error) {
	opts := cluster.Spec.Provider.Kubernetes

	_, restConfig, dynClient, k8sProvider, err := buildKubernetesInfra(opts)
	if err != nil {
		return nil, nil, err
	}

	// Use kwokConfig.Name as the cluster name — it's always set correctly
	// by applyClusterNameOverride, while cluster.Metadata.Name may be empty
	// when using --name flag without a ksail.yaml file.
	clusterName := kwokConfig.Name

	provisioner := kwokprovisioner.NewKubernetesProvisioner(
		kwokprovisioner.KubernetesProvisionerConfig{
			Name:             kwokConfig.Name,
			ConfigPath:       kwokConfig.ConfigPath,
			KubeconfigPath:   cluster.Spec.Cluster.Connection.Kubeconfig,
			K8sProvider:      k8sProvider,
			DynamicClient:    dynClient,
			RestConfig:       restConfig,
			ClusterName:      clusterName,
			Distribution:     string(cluster.Spec.Cluster.Distribution),
			GatewayClassName: opts.GatewayClassName,
		},
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
