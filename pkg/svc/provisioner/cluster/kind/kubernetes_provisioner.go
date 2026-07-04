package kindprovisioner

import (
	"context"
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	kubernetesprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/nested"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// KubernetesProvisioner wraps a Kind provisioner with DinD lifecycle management.
// It port-forwards the DinD pod's Docker API to localhost, sets DOCKER_HOST so
// the Kind SDK transparently creates containers inside DinD, then port-forwards
// the nested API server for host access.
type KubernetesProvisioner struct {
	*Provisioner

	hostClientset    kubernetes.Interface
	k8sProvider      *kubernetesprovider.Provider
	dynamicClient    dynamic.Interface
	restConfig       *rest.Config
	distribution     string
	gatewayClassName string
	hostContext      string
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
	// HostClientset is the Kubernetes clientset for the host cluster, used to publish and read the
	// nested cluster's kubeconfig Secret for the operator Connector contract.
	HostClientset kubernetes.Interface
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
	// HostContext is the explicitly-configured host kubeconfig context (empty = resolved from
	// the kubeconfig's current-context).
	HostContext string
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
		hostClientset:    cfg.HostClientset,
		k8sProvider:      cfg.K8sProvider,
		dynamicClient:    cfg.DynamicClient,
		restConfig:       cfg.RestConfig,
		distribution:     cfg.Distribution,
		gatewayClassName: cfg.GatewayClassName,
		hostContext:      cfg.HostContext,
		apiServerPort:    cfg.APIServerPort,
		kubeconfigPath:   kubeconfigPath,
		persistence:      cfg.Persistence,
	}, nil
}

// Create creates a Kind cluster inside a DinD pod on the host Kubernetes cluster.
// It port-forwards the DinD Docker API, sets DOCKER_HOST, then delegates to the
// inner Kind provisioner which uses the Kind SDK (Cobra commands that shell out
// to the docker CLI, inheriting DOCKER_HOST).
func (p *KubernetesProvisioner) Create(
	ctx context.Context,
	name string,
) error {
	target := setName(name, p.kindConfig.Name)

	// Preserve the host kubeconfig's current-context (the Kind SDK switches it to "kind-<name>"
	// on create) when the host is resolved from current-context. With an explicit host context
	// configured, leave the user pointed at the new nested cluster.
	restoreContext, err := k8s.PreserveCurrentContextUnlessExplicit(p.kubeconfigPath, p.hostContext)
	if err != nil {
		return fmt.Errorf("preserve host kubeconfig context: %w", err)
	}

	defer restoreContext()

	// Step 1: Ensure namespace + DinD pod
	err = p.setupDinD(ctx, target)
	if err != nil {
		return err
	}

	// Step 2: Resolve a stable, server-side exposure (Gateway → NodePort) for the nested API
	// server. The LoadBalancer tier is skipped to avoid the host LB controller (e.g. K3s
	// klipper-lb) binding the API server port on the node. This address survives the CLI process
	// exit and is written to the kubeconfig, so no long-lived port-forward is needed.
	exposure, err := p.k8sProvider.ResolveExposure(
		ctx, p.dynamicClient,
		kubernetesprovider.APIExposureSpec{
			ClusterName:      target,
			APIPort:          p.apiServerPort,
			GatewayClassName: p.gatewayClassName,
			HostAddress:      p.restConfig.Host,
			SkipLoadBalancer: true,
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

	// Step 6: Point the kubeconfig at the stable exposure address and publish it as a host-cluster
	// Secret so the operator's Connector can install components into the child cluster.
	err = p.finalizeKubeconfig(ctx, target, exposure.ServerURL())
	if err != nil {
		return err
	}

	return nil
}

// Delete deletes the Kind cluster inside DinD and cleans up host cluster resources.
// The inner Kind SDK delete uses the resolved target name (unlike KWOK, which uses
// the raw name).
func (p *KubernetesProvisioner) Delete(ctx context.Context, name string) error {
	target := setName(name, p.kindConfig.Name)

	//nolint:wrapcheck // DinDLifecycle.Delete already wraps with teardown/cleanup context
	return p.dindLifecycle().Delete(ctx, target, "kind-"+target, func() error {
		return p.Provisioner.Delete(ctx, target)
	})
}

// Exists checks if the Kind-on-Kubernetes cluster exists by checking for the DinD pod.
func (p *KubernetesProvisioner) Exists(ctx context.Context, name string) (bool, error) {
	target := setName(name, p.kindConfig.Name)

	//nolint:wrapcheck // DinDLifecycle.Exists already wraps with "check nodes" context
	return p.dindLifecycle().Exists(ctx, target)
}

// List returns cluster names found by namespace.
func (p *KubernetesProvisioner) List(ctx context.Context) ([]string, error) {
	//nolint:wrapcheck // DinDLifecycle.List already wraps with "list clusters" context
	return p.dindLifecycle().List(ctx)
}

// setupDinD creates the namespace and DinD pod, then waits for readiness.
func (p *KubernetesProvisioner) setupDinD(ctx context.Context, clusterName string) error {
	//nolint:wrapcheck // DinDLifecycle.SetupDinD already wraps with "setup DinD" context
	return p.dindLifecycle().SetupDinD(ctx, clusterName, p.distribution, p.persistence)
}

// dindLifecycle bundles the shared DinD delete/exists/list/setup flow for this
// Kind-on-Kubernetes provisioner.
func (p *KubernetesProvisioner) dindLifecycle() nested.DinDLifecycle {
	return nested.DinDLifecycle{
		Provider:       p.k8sProvider,
		DynamicClient:  p.dynamicClient,
		RestConfig:     p.restConfig,
		KubeconfigPath: p.kubeconfigPath,
		LogWriter:      os.Stdout,
	}
}

// finalizeKubeconfig points the on-disk kubeconfig at the stable exposure address and then publishes
// the nested cluster's kubeconfig as a host-cluster Secret for the operator Connector (see
// connector.go). It groups the two kubeconfig-finalization steps that close out Create.
func (p *KubernetesProvisioner) finalizeKubeconfig(
	ctx context.Context,
	target, serverURL string,
) error {
	err := p.rewriteKindKubeconfig(target, serverURL)
	if err != nil {
		return fmt.Errorf("rewrite kubeconfig: %w", err)
	}

	err = p.publishConnectorKubeconfig(ctx, target)
	if err != nil {
		return fmt.Errorf("publish connector kubeconfig: %w", err)
	}

	return nil
}

// publishConnectorKubeconfig minifies the shared host kubeconfig down to the nested Kind cluster's
// context and publishes it as a Secret in the DinD namespace under the Connection naming contract,
// so Kubeconfig() can serve it back to the operator. The on-disk kubeconfig is already pointed at the
// operator-reachable exposure address (a cert SAN), so it is published as-is after minifying. The
// write is idempotent, so re-creating a cluster of the same name refreshes the credentials in place.
func (p *KubernetesProvisioner) publishConnectorKubeconfig(
	ctx context.Context,
	target string,
) error {
	conn := ConnectionFor(target)

	raw, err := nested.ExtractContextKubeconfig(p.kubeconfigPath, conn.ContextName)
	if err != nil {
		return fmt.Errorf("extract nested kubeconfig: %w", err)
	}

	err = nested.PublishKubeconfigSecret(
		ctx, p.hostClientset,
		conn.Namespace, conn.SecretName, kindKubeconfigKey,
		raw, kubernetesprovider.CommonLabels(target),
	)
	if err != nil {
		return fmt.Errorf("publish kubeconfig secret: %w", err)
	}

	return nil
}

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
