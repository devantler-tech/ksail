package kindprovisioner

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil/marshaller"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	kubernetessprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// kindVersion is the Kind binary version installed inside DinD pods.
// Must match go.mod: sigs.k8s.io/kind.
const kindVersion = "v0.31.0"

// KubernetesProvisioner wraps a Kind provisioner with DinD lifecycle management.
// Instead of running the Kind SDK from the host (which requires a port-forward to
// the DinD Docker daemon), it installs the Kind binary inside the DinD pod and
// runs `kind create cluster` via kubectl exec. This avoids SPDY tunnel reliability
// issues that cause Docker API calls (especially docker exec) to fail.
type KubernetesProvisioner struct {
	*Provisioner

	k8sProvider      *kubernetessprovider.Provider
	dynamicClient    dynamic.Interface
	restConfig       *rest.Config
	clusterName      string
	distribution     string
	gatewayClassName string
	apiServerPort    int32
	kubeconfigPath   string
	portForward      *kubernetessprovider.PortForwardSession
}

// KubernetesProvisionerConfig holds configuration for creating a KubernetesProvisioner.
type KubernetesProvisionerConfig struct {
	// KindConfig is the Kind cluster configuration.
	KindConfig *v1alpha4.Cluster
	// KubeconfigPath is the path to the nested cluster's kubeconfig.
	KubeconfigPath string
	// K8sProvider is the Kubernetes infrastructure provider.
	K8sProvider *kubernetessprovider.Provider
	// DynamicClient is the dynamic client for Gateway API resources.
	DynamicClient dynamic.Interface
	// RestConfig is the REST config for exec into the DinD pod.
	RestConfig *rest.Config
	// ClusterName is the nested cluster name.
	ClusterName string
	// Distribution is the distribution name (for labels).
	Distribution string
	// GatewayClassName is the Gateway class for API exposure (empty = no gateway).
	GatewayClassName string
	// APIServerPort is the port the nested API server listens on.
	APIServerPort int32
}

// NewKubernetesProvisioner creates a KubernetesProvisioner that wraps Kind with DinD lifecycle.
func NewKubernetesProvisioner(cfg KubernetesProvisionerConfig) (*KubernetesProvisioner, error) {
	kubeconfigPath := cfg.KubeconfigPath
	if kubeconfigPath == "" {
		kubeconfigPath = k8s.DefaultKubeconfigPath()
	} else if strings.HasPrefix(kubeconfigPath, "~/") {
		homeDir, _ := os.UserHomeDir()
		if homeDir != "" {
			kubeconfigPath = homeDir + kubeconfigPath[1:]
		}
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
		clusterName:      cfg.ClusterName,
		distribution:     cfg.Distribution,
		gatewayClassName: cfg.GatewayClassName,
		apiServerPort:    cfg.APIServerPort,
		kubeconfigPath:   kubeconfigPath,
	}, nil
}

// Create creates a Kind cluster inside a DinD pod on the host Kubernetes cluster.
// The Kind binary is installed and executed inside the DinD pod via kubectl exec,
// avoiding SPDY port-forward reliability issues with Docker API streaming.
func (p *KubernetesProvisioner) Create(ctx context.Context, name string) error {
	target := setName(name, p.Provisioner.kindConfig.Name)

	// Step 1: Ensure namespace exists
	err := p.k8sProvider.EnsureNamespace(ctx, p.clusterName)
	if err != nil {
		return fmt.Errorf("ensure namespace: %w", err)
	}

	// Step 2: Create DinD pod and service
	err = p.k8sProvider.CreateDinDPod(ctx, p.clusterName, p.distribution)
	if err != nil {
		return fmt.Errorf("create DinD pod: %w", err)
	}

	// Step 3: Wait for DinD to be ready (Docker daemon accepting connections)
	err = p.k8sProvider.WaitForDinD(ctx, p.clusterName)
	if err != nil {
		return fmt.Errorf("wait for DinD: %w", err)
	}

	// Step 4: Install Kind binary inside the DinD pod
	fmt.Fprintln(os.Stdout, "► installing Kind binary in DinD pod")

	err = p.k8sProvider.InstallKindInDinD(ctx, p.restConfig, p.clusterName, kindVersion)
	if err != nil {
		return fmt.Errorf("install Kind in DinD: %w", err)
	}

	// Step 5: Marshal Kind config and create cluster inside DinD
	m := marshaller.NewYAMLMarshaller[*v1alpha4.Cluster]()

	configYAML, err := m.Marshal(p.Provisioner.kindConfig)
	if err != nil {
		return fmt.Errorf("marshal kind config: %w", err)
	}

	fmt.Fprintln(os.Stdout, "► creating Kind cluster inside DinD pod")

	kubeconfigContent, err := p.k8sProvider.RunKindCreateInDinD(
		ctx, p.restConfig, p.clusterName, target, configYAML,
	)
	if err != nil {
		return fmt.Errorf("kind create in DinD: %w", err)
	}

	// Step 6: Start port-forward to the nested API server so kubeconfig works from the host
	fmt.Fprintln(os.Stdout, "► port-forwarding nested API server to localhost")

	pf, err := p.k8sProvider.StartPortForward(
		ctx, p.restConfig, p.clusterName, kubernetessprovider.DinDPodName, int(p.apiServerPort),
	)
	if err != nil {
		return fmt.Errorf("port-forward API server: %w", err)
	}

	p.portForward = pf

	// Step 7: Rewrite kubeconfig server address from 0.0.0.0:6443 to 127.0.0.1:<local-port>
	kubeconfigContent = strings.ReplaceAll(
		kubeconfigContent,
		fmt.Sprintf("https://0.0.0.0:%d", p.apiServerPort),
		fmt.Sprintf("https://127.0.0.1:%d", pf.LocalPort),
	)

	// Step 8: Merge kubeconfig into the host kubeconfig file
	if kubeconfigContent != "" && p.kubeconfigPath != "" {
		err = k8s.MergeKubeconfig(p.kubeconfigPath, []byte(kubeconfigContent))
		if err != nil {
			return fmt.Errorf("merge kubeconfig: %w", err)
		}
	}

	// Step 9: Expose the nested API server via Gateway API (if configured)
	err = p.k8sProvider.EnsureAPIExposure(
		ctx,
		p.dynamicClient,
		p.clusterName,
		p.apiServerPort,
		p.gatewayClassName,
	)
	if err != nil {
		return fmt.Errorf("expose API server: %w", err)
	}

	return nil
}

// Delete deletes the Kind cluster inside DinD and cleans up host cluster resources.
func (p *KubernetesProvisioner) Delete(ctx context.Context, name string) error {
	target := setName(name, p.Provisioner.kindConfig.Name)

	// Close port-forward if active
	if p.portForward != nil {
		p.portForward.Close()
	}

	// Try to delete the Kind cluster inside DinD first (best-effort)
	_ = p.k8sProvider.RunKindDeleteInDinD(ctx, p.restConfig, p.clusterName, target)

	// Clean up API exposure resources
	if err := p.k8sProvider.DeleteAPIExposure(ctx, p.dynamicClient, p.clusterName); err != nil {
		return fmt.Errorf("delete API exposure: %w", err)
	}

	// Clean up DinD
	if err := p.k8sProvider.DeleteDinD(ctx, p.clusterName); err != nil {
		return fmt.Errorf("delete DinD: %w", err)
	}

	// Delete namespace (cascading delete)
	if err := p.k8sProvider.DeleteNodes(ctx, p.clusterName); err != nil {
		return fmt.Errorf("delete namespace: %w", err)
	}

	return nil
}
