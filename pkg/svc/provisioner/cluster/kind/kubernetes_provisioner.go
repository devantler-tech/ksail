package kindprovisioner

import (
	"context"
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	kubernetesprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// KubernetesProvisioner wraps a Kind provisioner with DinD lifecycle management.
// It port-forwards the DinD pod's Docker API to localhost, sets DOCKER_HOST so
// the Kind SDK transparently creates containers inside DinD, then port-forwards
// the nested API server for host access.
type KubernetesProvisioner struct {
	*Provisioner

	k8sProvider      *kubernetesprovider.Provider
	dynamicClient    dynamic.Interface
	restConfig       *rest.Config
	distribution     string
	gatewayClassName string
	apiServerPort    int32
	kubeconfigPath   string
	persistence      v1alpha1.KubernetesPersistence
}

// KubernetesProvisionerConfig holds configuration for creating a KubernetesProvisioner.
type KubernetesProvisionerConfig struct {
	// KindConfig is the Kind cluster configuration.
	KindConfig *v1alpha4.Cluster
	// KubeconfigPath is the path to the nested cluster's kubeconfig.
	KubeconfigPath string
	// K8sProvider is the Kubernetes infrastructure provider.
	K8sProvider *kubernetesprovider.Provider
	// DynamicClient is the dynamic client for Gateway API resources.
	DynamicClient dynamic.Interface
	// RestConfig is the REST config for port-forwarding to the DinD pod.
	RestConfig *rest.Config
	// ClusterName is the nested cluster name.
	ClusterName string
	// Distribution is the distribution name (for labels).
	Distribution string
	// GatewayClassName is the Gateway class for API exposure (empty = no gateway).
	GatewayClassName string
	// APIServerPort is the port the nested API server listens on.
	APIServerPort int32
	// Persistence holds PVC configuration for the DinD Docker data directory.
	Persistence v1alpha1.KubernetesPersistence
}

// NewKubernetesProvisioner creates a KubernetesProvisioner that wraps Kind with DinD lifecycle.
func NewKubernetesProvisioner(cfg KubernetesProvisionerConfig) (*KubernetesProvisioner, error) {
	kubeconfigPath, err := k8s.ResolveKubeconfigPath(cfg.KubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("resolve kubeconfig path: %w", err)
	}

	// For Kubernetes provider, inject the K8s provider as the infra provider
	innerProvisioner, err := CreateProvisionerWithProvider(
		cfg.KindConfig,
		kubeconfigPath,
		cfg.K8sProvider,
	)
	if err != nil {
		return nil, fmt.Errorf("create inner Kind provisioner: %w", err)
	}

	return &KubernetesProvisioner{
		Provisioner:      innerProvisioner,
		k8sProvider:      cfg.K8sProvider,
		dynamicClient:    cfg.DynamicClient,
		restConfig:       cfg.RestConfig,
		distribution:     cfg.Distribution,
		gatewayClassName: cfg.GatewayClassName,
		apiServerPort:    cfg.APIServerPort,
		kubeconfigPath:   kubeconfigPath,
		persistence:      cfg.Persistence,
	}, nil
}

// Create creates a Kind cluster inside a DinD pod on the host Kubernetes cluster.
// It port-forwards the DinD Docker API, sets DOCKER_HOST, then delegates to the
// inner Kind provisioner which uses the Kind SDK (Cobra commands that shell out
// to the docker CLI, inheriting DOCKER_HOST).
//
//nolint:funlen // sequential setup steps with many error-checks
func (p *KubernetesProvisioner) Create(
	ctx context.Context,
	name string,
) error {
	target := setName(name, p.kindConfig.Name)

	// Preserve the host kubeconfig's current-context. The Kind SDK switches
	// current-context to "kind-<name>" when it creates the cluster, which would
	// cause subsequent Kubernetes provider operations to connect to the nested
	// cluster instead of the host cluster.
	originalContext, err := k8s.GetKubeconfigCurrentContext(p.kubeconfigPath)
	if err != nil {
		return fmt.Errorf("read current kubeconfig context: %w", err)
	}

	// Restore the context on any return path (success or failure).
	defer func() {
		restoreErr := k8s.SetKubeconfigCurrentContext(p.kubeconfigPath, originalContext)
		if restoreErr != nil {
			_, _ = fmt.Fprintf(
				os.Stderr,
				"warning: failed to restore kubeconfig context: %v\n",
				restoreErr,
			)
		}
	}()

	// Step 1: Ensure namespace + DinD pod
	err = p.setupDinD(ctx, target)
	if err != nil {
		return err
	}

	// Step 2: Resolve a stable, server-side exposure (Gateway → LoadBalancer → NodePort) for the
	// nested API server. This address survives the CLI process exit and is written to the
	// kubeconfig, so no long-lived port-forward is needed for steady-state access.
	exposure, err := p.k8sProvider.ResolveExposure(
		ctx, p.dynamicClient,
		kubernetesprovider.APIExposureSpec{
			ClusterName:      target,
			APIPort:          p.apiServerPort,
			GatewayClassName: p.gatewayClassName,
			HostAddress:      p.restConfig.Host,
		},
	)
	if err != nil {
		return fmt.Errorf("expose API server: %w", err)
	}

	// Step 3: Add the exposure address to the API server cert SANs so kubectl verifies TLS when
	// connecting via the stable address (the kubeadm patch also keeps 127.0.0.1/localhost).
	appendAPIServerCertSAN(p.kindConfig, exposure.Address)

	// Step 4: Start exec tunnel for Docker API (2375) to localhost.
	// The exec tunnel uses CRI exec + nc instead of SPDY port-forward,
	// which correctly handles Docker's HTTP connection hijacking (101 Upgrade)
	// for docker exec operations.
	dockerPF, err := p.k8sProvider.StartExecTunnel(
		ctx, p.restConfig, target,
		kubernetesprovider.DinDPodName, kubernetesprovider.DinDContainerName,
		kubernetesprovider.DinDDockerPort,
	)
	if err != nil {
		return fmt.Errorf("port-forward Docker API: %w", err)
	}
	defer dockerPF.Close()

	// Step 5: Set DOCKER_HOST so the Kind SDK talks to DinD
	msg := "► creating Kind cluster via SDK (DOCKER_HOST → exec tunnel → DinD)"
	_, _ = fmt.Fprintln(os.Stdout, msg)

	err = kubernetesprovider.WithRemoteDockerHost(dockerPF, func() error {
		return p.Provisioner.Create(ctx, target)
	})
	if err != nil {
		return fmt.Errorf("kind create via SDK: %w", err)
	}

	// Step 6: Point the kubeconfig at the stable exposure address.
	err = p.rewriteKindKubeconfig(target, exposure.ServerURL())
	if err != nil {
		return fmt.Errorf("rewrite kubeconfig: %w", err)
	}

	return nil
}

// Delete deletes the Kind cluster inside DinD and cleans up host cluster resources.
func (p *KubernetesProvisioner) Delete(ctx context.Context, name string) error {
	target := setName(name, p.kindConfig.Name)

	// jscpd:ignore-start
	// Best-effort: delete Kind cluster inside DinD via SDK
	dockerPF, pfErr := p.k8sProvider.StartExecTunnel(
		ctx, p.restConfig, target,
		kubernetesprovider.DinDPodName, kubernetesprovider.DinDContainerName,
		kubernetesprovider.DinDDockerPort,
	)
	if pfErr == nil {
		defer dockerPF.Close()

		_ = kubernetesprovider.WithRemoteDockerHost(dockerPF, func() error {
			return p.Provisioner.Delete(ctx, target)
		})
	}

	// Clean up API exposure, DinD, and namespace
	err := p.k8sProvider.TeardownDinD(ctx, p.dynamicClient, target)
	if err != nil {
		return fmt.Errorf("teardown DinD: %w", err)
	}

	// Clean up kubeconfig entries
	contextName := "kind-" + target

	err = k8s.CleanupKubeconfig(p.kubeconfigPath, contextName, contextName, contextName, os.Stdout)
	if err != nil {
		return fmt.Errorf("cleanup kubeconfig: %w", err)
	}

	return nil
}

// Exists checks if the Kind-on-Kubernetes cluster exists by checking for the DinD pod.
func (p *KubernetesProvisioner) Exists(ctx context.Context, name string) (bool, error) {
	target := setName(name, p.kindConfig.Name)

	exists, err := p.k8sProvider.NodesExist(ctx, target)
	if err != nil {
		return false, fmt.Errorf("check nodes: %w", err)
	}

	return exists, nil
}

// List returns cluster names found by namespace.
func (p *KubernetesProvisioner) List(ctx context.Context) ([]string, error) {
	clusters, err := p.k8sProvider.ListAllClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}

	return clusters, nil
}

// setupDinD creates the namespace and DinD pod, then waits for readiness.
func (p *KubernetesProvisioner) setupDinD(ctx context.Context, clusterName string) error {
	err := p.k8sProvider.SetupDinD(ctx, clusterName, p.distribution, p.persistence)
	if err != nil {
		return fmt.Errorf("setup DinD: %w", err)
	}

	return nil
}

// jscpd:ignore-end

// rewriteKindKubeconfig rewrites the Kind kubeconfig server URL to the stable exposure address.
// Kind writes kubeconfig with context "kind-<name>" and cluster entry "kind-<name>".
func (p *KubernetesProvisioner) rewriteKindKubeconfig(clusterName, serverURL string) error {
	clusterKey := "kind-" + clusterName

	err := k8s.ModifyKubeconfigCluster(
		p.kubeconfigPath,
		clusterKey,
		serverURL,
	)
	if err != nil {
		return fmt.Errorf("modify kubeconfig cluster: %w", err)
	}

	return nil
}

// appendAPIServerCertSAN appends a kubeadm certSANs patch (including the resolved exposure
// address alongside the loopback SANs) to every control-plane node. Kind applies
// kubeadmConfigPatches as strategic merge patches, so the certSANs list in this later patch
// supersedes the base patch from applyKindDinDNetworking — hence it must carry the full list.
func appendAPIServerCertSAN(kindConfig *v1alpha4.Cluster, san string) {
	if san == "" {
		return
	}

	patch := fmt.Sprintf(`kind: ClusterConfiguration
apiServer:
  certSANs:
  - "127.0.0.1"
  - "localhost"
  - "0.0.0.0"
  - %q`, san)

	hasControlPlane := false

	for i := range kindConfig.Nodes {
		if kindConfig.Nodes[i].Role == v1alpha4.ControlPlaneRole {
			kindConfig.Nodes[i].KubeadmConfigPatches = append(
				kindConfig.Nodes[i].KubeadmConfigPatches,
				patch,
			)
			hasControlPlane = true
		}
	}

	if !hasControlPlane {
		kindConfig.Nodes = append(kindConfig.Nodes, v1alpha4.Node{
			Role:                 v1alpha4.ControlPlaneRole,
			KubeadmConfigPatches: []string{patch},
		})
	}
}
