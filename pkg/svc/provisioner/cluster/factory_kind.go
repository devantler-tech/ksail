package clusterprovisioner

import (
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	kindconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/kind"
	kubernetesprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	kindprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kind"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

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

	// Enable the MutatingAdmissionPolicy feature gate / v1beta1 admissionregistration
	// API only for Calico, whose v3.30+ CRD chart ships MutatingAdmissionPolicy
	// resources. Enabling it elsewhere makes other components (e.g. Kyverno) attempt to
	// use the API and fail.
	if cluster.Spec.Cluster.CNI == v1alpha1.CNICalico {
		kindconfigmanager.ApplyAPIServerFeatureGates(kindConfig)
	}

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
	// while cluster.Name may be empty with --name flag.
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
			HostContext:      resolveKubernetesOption(opts.Context, opts.ContextEnvVar),
			APIServerPort:    kubernetesprovider.DinDAPIServerPort,
			Persistence:      opts.Persistence,
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
