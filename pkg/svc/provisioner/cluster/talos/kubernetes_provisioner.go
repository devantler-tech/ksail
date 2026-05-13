package talosprovisioner

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	kubernetessprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// talosVersion is the talosctl binary version installed inside DinD pods.
// Must match go.mod: github.com/siderolabs/talos.
const talosVersion = "v1.13.0"

// KubernetesProvisioner wraps a Talos provisioner with DinD lifecycle management.
// It installs the talosctl binary inside the DinD pod and runs
// `talosctl cluster create --docker` via kubectl exec.
type KubernetesProvisioner struct {
	k8sProvider      *kubernetessprovider.Provider
	dynamicClient    dynamic.Interface
	restConfig       *rest.Config
	clusterName      string
	distribution     string
	gatewayClassName string
	kubeconfigPath   string
	controlPlanes    int
	workers          int
	portForward      *kubernetessprovider.PortForwardSession
}

// KubernetesProvisionerConfig holds configuration for creating a KubernetesProvisioner.
type KubernetesProvisionerConfig struct {
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
	// ControlPlanes is the number of control-plane nodes.
	ControlPlanes int
	// Workers is the number of worker nodes.
	Workers int
}

// NewKubernetesProvisioner creates a KubernetesProvisioner that wraps Talos with DinD lifecycle.
func NewKubernetesProvisioner(cfg KubernetesProvisionerConfig) *KubernetesProvisioner {
	kubeconfigPath := cfg.KubeconfigPath
	if kubeconfigPath == "" {
		kubeconfigPath = k8s.DefaultKubeconfigPath()
	} else if strings.HasPrefix(kubeconfigPath, "~/") {
		homeDir, _ := os.UserHomeDir()
		if homeDir != "" {
			kubeconfigPath = homeDir + kubeconfigPath[1:]
		}
	}

	controlPlanes := cfg.ControlPlanes
	if controlPlanes < 1 {
		controlPlanes = 1
	}

	return &KubernetesProvisioner{
		k8sProvider:      cfg.K8sProvider,
		dynamicClient:    cfg.DynamicClient,
		restConfig:       cfg.RestConfig,
		clusterName:      cfg.ClusterName,
		distribution:     cfg.Distribution,
		gatewayClassName: cfg.GatewayClassName,
		kubeconfigPath:   kubeconfigPath,
		controlPlanes:    controlPlanes,
		workers:          cfg.Workers,
	}
}

// Create creates a Talos cluster inside a DinD pod on the host Kubernetes cluster.
// The talosctl binary is installed and executed inside the DinD pod via kubectl exec.
func (p *KubernetesProvisioner) Create(ctx context.Context, name string) error {
	clusterName := name
	if clusterName == "" {
		clusterName = p.clusterName
	}

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

	// Step 4: Install talosctl binary inside the DinD pod
	fmt.Fprintln(os.Stdout, "► installing talosctl binary in DinD pod")

	err = p.k8sProvider.InstallTalosctlInDinD(ctx, p.restConfig, p.clusterName, talosVersion)
	if err != nil {
		return fmt.Errorf("install talosctl in DinD: %w", err)
	}

	// Step 5: Create Talos cluster inside DinD
	fmt.Fprintln(os.Stdout, "► creating Talos cluster inside DinD pod")

	createResult, err := p.k8sProvider.RunTalosCreateInDinD(
		ctx, p.restConfig, p.clusterName, clusterName, p.controlPlanes, p.workers,
	)
	if err != nil {
		return fmt.Errorf("talos create in DinD: %w", err)
	}

	// Step 6: Port-forward the K8s API server to localhost using the mapped port
	fmt.Fprintln(os.Stdout, "► port-forwarding nested API server to localhost")

	pf, err := p.k8sProvider.StartPortForward(
		ctx, p.restConfig, p.clusterName, kubernetessprovider.DinDPodName, createResult.APIServerPort,
	)
	if err != nil {
		return fmt.Errorf("port-forward API server: %w", err)
	}

	p.portForward = pf

	// Step 7: Rewrite kubeconfig server address to use the host port-forward
	kubeconfigContent, err := rewriteTalosKubeconfig(
		createResult.Kubeconfig, pf.LocalPort, clusterName,
	)
	if err != nil {
		return fmt.Errorf("rewrite kubeconfig: %w", err)
	}

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
		int32(createResult.APIServerPort),
		p.gatewayClassName,
	)
	if err != nil {
		return fmt.Errorf("expose API server: %w", err)
	}

	return nil
}

// Delete deletes the Talos cluster inside DinD and cleans up host cluster resources.
func (p *KubernetesProvisioner) Delete(ctx context.Context, name string) error {
	clusterName := name
	if clusterName == "" {
		clusterName = p.clusterName
	}

	// Close port-forward if active
	if p.portForward != nil {
		p.portForward.Close()
	}

	// Try to destroy the Talos cluster inside DinD first (best-effort)
	_ = p.k8sProvider.RunTalosDeleteInDinD(ctx, p.restConfig, p.clusterName, clusterName)

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

// Exists checks if the Talos cluster exists by checking for the DinD pod.
func (p *KubernetesProvisioner) Exists(ctx context.Context, _ string) (bool, error) {
	return p.k8sProvider.NodesExist(ctx, p.clusterName)
}

// List returns cluster names found by namespace.
func (p *KubernetesProvisioner) List(ctx context.Context) ([]string, error) {
	return p.k8sProvider.ListAllClusters(ctx)
}

// Start is not supported for Talos-on-Kubernetes.
func (p *KubernetesProvisioner) Start(_ context.Context, _ string) error {
	return fmt.Errorf("start not supported for Talos-on-Kubernetes: recreate the cluster instead")
}

// Stop is not supported for Talos-on-Kubernetes.
func (p *KubernetesProvisioner) Stop(_ context.Context, _ string) error {
	return fmt.Errorf("stop not supported for Talos-on-Kubernetes: delete the cluster instead")
}

// rewriteTalosKubeconfig rewrites the Talos kubeconfig to use the localhost
// port-forward address and renames the context for uniqueness.
func rewriteTalosKubeconfig(kubeconfigContent string, localPort int, clusterName string) (string, error) {
	config, err := clientcmd.Load([]byte(kubeconfigContent))
	if err != nil {
		return "", fmt.Errorf("parse kubeconfig: %w", err)
	}

	// The talosctl kubeconfig uses context "admin@<cluster-name>".
	// Rewrite the server URL to point at the local port-forward.
	serverURL := fmt.Sprintf("https://127.0.0.1:%d", localPort)

	for _, cluster := range config.Clusters {
		cluster.Server = serverURL
	}

	out, err := clientcmd.Write(*config)
	if err != nil {
		return "", fmt.Errorf("serialize kubeconfig: %w", err)
	}

	return string(out), nil
}
