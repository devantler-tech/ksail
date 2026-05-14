package kwokprovisioner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	kubernetessprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// KubernetesProvisioner wraps a KWOK provisioner with DinD lifecycle management.
// It uses an exec tunnel for the DinD pod's Docker API, sets DOCKER_HOST so
// kwokctl (docker compose runtime) transparently creates containers inside DinD,
// then port-forwards the nested API server for host access.
type KubernetesProvisioner struct {
	*Provisioner

	k8sProvider      *kubernetessprovider.Provider
	dynamicClient    dynamic.Interface
	restConfig       *rest.Config
	clusterName      string
	distribution     string
	gatewayClassName string
	kubeconfigPath   string
	portForward      *kubernetessprovider.PortForwardSession
}

// KubernetesProvisionerConfig holds configuration for creating a KubernetesProvisioner.
type KubernetesProvisionerConfig struct {
	// Name is the KWOK cluster name passed to kwokctl.
	Name string
	// ConfigPath is the optional path to a kwok.yaml configuration file.
	ConfigPath string
	// KubeconfigPath is the path to the nested cluster's kubeconfig.
	KubeconfigPath string
	// K8sProvider is the Kubernetes infrastructure provider.
	K8sProvider *kubernetessprovider.Provider
	// DynamicClient is the dynamic client for Gateway API resources.
	DynamicClient dynamic.Interface
	// RestConfig is the REST config for port-forwarding to the DinD pod.
	RestConfig *rest.Config
	// ClusterName is the nested cluster name (used for namespace, labels).
	ClusterName string
	// Distribution is the distribution name (for labels).
	Distribution string
	// GatewayClassName is the Gateway class for API exposure (empty = no gateway).
	GatewayClassName string
}

// NewKubernetesProvisioner creates a KubernetesProvisioner that wraps KWOK with DinD lifecycle.
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

	innerProvisioner := NewProvisioner(
		cfg.Name,
		cfg.ConfigPath,
		cfg.K8sProvider,
	)

	return &KubernetesProvisioner{
		Provisioner:      innerProvisioner,
		k8sProvider:      cfg.K8sProvider,
		dynamicClient:    cfg.DynamicClient,
		restConfig:       cfg.RestConfig,
		clusterName:      cfg.ClusterName,
		distribution:     cfg.Distribution,
		gatewayClassName: cfg.GatewayClassName,
		kubeconfigPath:   kubeconfigPath,
	}
}

// kwokContainerNames returns the Docker container names kwokctl creates inside DinD.
// Etcd is excluded because it has no host-path bind mounts.
func kwokContainerNames(clusterName string) []string {
	prefix := "kwok-" + clusterName
	return []string{
		prefix + "-kube-apiserver",
		prefix + "-kube-controller-manager",
		prefix + "-kube-scheduler",
		prefix + "-kwok-controller",
	}
}

// Create creates a KWOK cluster inside a DinD pod on the host Kubernetes cluster.
func (p *KubernetesProvisioner) Create(ctx context.Context, name string) error {
	target := p.Provisioner.resolveName(name)

	// Step 1: Ensure namespace + DinD pod
	if err := p.setupDinD(ctx); err != nil {
		return err
	}

	// Step 2: Pre-create placeholder files in DinD at the exact paths kwokctl
	// will reference in Docker bind mounts. Without this, Docker auto-creates
	// missing bind mount sources as directories, causing kube-apiserver to fail
	// with "read /etc/kubernetes/pki/admin.key: is a directory".
	fmt.Fprintln(os.Stdout, "► preparing DinD bind mount paths")

	if err := p.createPlaceholderFiles(ctx, target); err != nil {
		return fmt.Errorf("create placeholder files: %w", err)
	}

	// Step 3: Start exec tunnel for Docker API (2375)
	dockerPF, err := p.k8sProvider.StartExecTunnel(
		ctx, p.restConfig, p.clusterName,
		kubernetessprovider.DinDPodName, kubernetessprovider.DinDContainerName,
		kubernetessprovider.DinDDockerPort,
	)
	if err != nil {
		return fmt.Errorf("exec tunnel Docker API: %w", err)
	}

	defer dockerPF.Close()

	// Step 4: Create the KWOK cluster via kwokctl SDK with DOCKER_HOST → DinD.
	// kwokctl generates PKI locally, writes Docker Compose containers inside DinD.
	// Containers are created with file-level bind mounts (thanks to placeholders)
	// but crash-loop because the placeholder files are empty.
	fmt.Fprintln(os.Stdout, "► creating KWOK cluster via SDK (DOCKER_HOST → exec tunnel → DinD)")

	err = kubernetessprovider.WithRemoteDockerHost(dockerPF, func() error {
		return p.Provisioner.CreateCluster(ctx, name)
	})
	if err != nil {
		return fmt.Errorf("kwok create via SDK: %w", err)
	}

	// Step 5: Copy real PKI, kubeconfig, and kwok.yaml from the local host
	// (where kwokctl wrote them) into DinD, overwriting the placeholders.
	fmt.Fprintln(os.Stdout, "► syncing PKI and config into DinD")

	if err := p.copyStateToDinD(ctx, target); err != nil {
		return fmt.Errorf("copy state to DinD: %w", err)
	}

	// Step 6: Restart crash-looping containers so they pick up the real files.
	// Bind mounts reference the DinD filesystem; a restart rereads the content.
	fmt.Fprintln(os.Stdout, "► restarting KWOK containers in DinD")

	if err := p.restartContainersInDinD(ctx, target); err != nil {
		return fmt.Errorf("restart containers: %w", err)
	}

	// Step 7: Discover the API server port from the kubeconfig that kwokctl wrote.
	apiServerPort, err := p.discoverAPIServerPort(name)
	if err != nil {
		return fmt.Errorf("discover API server port: %w", err)
	}

	// Step 8: Port-forward the nested API server from DinD to localhost.
	fmt.Fprintln(os.Stdout, "► port-forwarding nested API server to localhost")

	pf, err := p.k8sProvider.StartPortForward(
		ctx, p.restConfig, p.clusterName,
		kubernetessprovider.DinDPodName, apiServerPort,
	)
	if err != nil {
		return fmt.Errorf("port-forward API server: %w", err)
	}

	p.portForward = pf

	// Step 9: Rewrite kubeconfig server URL to use the local port-forward address
	if err := p.rewriteKubeconfig(name, pf.LocalPort); err != nil {
		return fmt.Errorf("rewrite kubeconfig: %w", err)
	}

	// Step 10: Wait for the nested API server to be reachable through port-forward.
	fmt.Fprintln(os.Stdout, "► waiting for nested API server to be ready")

	if err := k8s.WaitForAPIServer(p.kubeconfigPath, "kwok-"+target); err != nil {
		return fmt.Errorf("wait for API server: %w", err)
	}

	// Step 11: Scale simulated nodes — now the API server is accessible via port-forward
	fmt.Fprintln(os.Stdout, "► creating simulated KWOK node")

	if err := p.Provisioner.ScaleNodes(ctx, name); err != nil {
		return fmt.Errorf("kwok scale via SDK: %w", err)
	}

	// Step 12: Expose via Gateway API (if configured)
	return p.k8sProvider.EnsureAPIExposure(
		ctx, p.dynamicClient, p.clusterName,
		int32(apiServerPort), p.gatewayClassName,
	)
}

// Delete deletes the KWOK cluster inside DinD and cleans up host cluster resources.
func (p *KubernetesProvisioner) Delete(ctx context.Context, name string) error {
	// Close port-forward if active
	if p.portForward != nil {
		p.portForward.Close()
	}

	// Best-effort: delete KWOK cluster inside DinD via SDK
	dockerPF, pfErr := p.k8sProvider.StartExecTunnel(
		ctx, p.restConfig, p.clusterName,
		kubernetessprovider.DinDPodName, kubernetessprovider.DinDContainerName,
		kubernetessprovider.DinDDockerPort,
	)
	if pfErr == nil {
		defer dockerPF.Close()

		_ = kubernetessprovider.WithRemoteDockerHost(dockerPF, func() error {
			return p.Provisioner.Delete(ctx, name)
		})
	}

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

	// Clean up kubeconfig entries
	target := p.Provisioner.resolveName(name)
	contextName := "kwok-" + target
	if err := k8s.CleanupKubeconfig(p.kubeconfigPath, contextName, contextName, contextName, os.Stdout); err != nil {
		return fmt.Errorf("cleanup kubeconfig: %w", err)
	}

	return nil
}

// Exists checks if the KWOK-on-Kubernetes cluster exists by checking for the DinD pod.
func (p *KubernetesProvisioner) Exists(ctx context.Context, _ string) (bool, error) {
	return p.k8sProvider.NodesExist(ctx, p.clusterName)
}

// List returns cluster names found by namespace.
func (p *KubernetesProvisioner) List(ctx context.Context) ([]string, error) {
	return p.k8sProvider.ListAllClusters(ctx)
}

// setupDinD creates the namespace and DinD pod, then waits for readiness.
func (p *KubernetesProvisioner) setupDinD(ctx context.Context) error {
	if err := p.k8sProvider.EnsureNamespace(ctx, p.clusterName); err != nil {
		return fmt.Errorf("ensure namespace: %w", err)
	}

	if err := p.k8sProvider.CreateDinDPod(ctx, p.clusterName, p.distribution); err != nil {
		return fmt.Errorf("create DinD pod: %w", err)
	}

	if err := p.k8sProvider.WaitForDinD(ctx, p.clusterName); err != nil {
		return fmt.Errorf("wait for DinD: %w", err)
	}

	return nil
}

// discoverAPIServerPort reads the kubeconfig that kwokctl wrote and extracts
// the API server port from the cluster entry. kwokctl (docker compose runtime)
// maps the API server's internal port 6443 to a random host port inside DinD
// and writes that random port into the kubeconfig.
func (p *KubernetesProvisioner) discoverAPIServerPort(name string) (int, error) {
	config, err := clientcmd.LoadFromFile(p.kubeconfigPath)
	if err != nil {
		return 0, fmt.Errorf("load kubeconfig: %w", err)
	}

	target := p.Provisioner.resolveName(name)
	clusterKey := "kwok-" + target

	cluster, ok := config.Clusters[clusterKey]
	if !ok {
		return 0, fmt.Errorf("cluster entry %q not found in kubeconfig", clusterKey)
	}

	// Server URL is typically https://127.0.0.1:<port>
	var port int

	_, err = fmt.Sscanf(cluster.Server, "https://127.0.0.1:%d", &port)
	if err != nil {
		return 0, fmt.Errorf("parse API server port from %q: %w", cluster.Server, err)
	}

	return port, nil
}

// rewriteKubeconfig rewrites the KWOK kubeconfig server URL to use the
// local port-forward address. Updates both:
//   - ~/.kube/config — the standard kubeconfig for kubectl access
//   - ~/.kwok/clusters/<name>/kubeconfig.yaml — kwokctl's internal kubeconfig
//     used by kwokctl scale and other runtime commands
func (p *KubernetesProvisioner) rewriteKubeconfig(name string, localPort int) error {
	target := p.Provisioner.resolveName(name)
	clusterKey := "kwok-" + target
	newServer := fmt.Sprintf("https://127.0.0.1:%d", localPort)

	// Rewrite ~/.kube/config (atomic, merge-safe)
	if err := k8s.ModifyKubeconfigCluster(p.kubeconfigPath, clusterKey, newServer); err != nil {
		return fmt.Errorf("rewrite kubeconfig: %w", err)
	}

	// Rewrite kwokctl's internal kubeconfig (dedicated file, not shared)
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}

	kwokKubeconfig := homeDir + "/.kwok/clusters/" + target + "/kubeconfig.yaml"

	kwokConfig, err := clientcmd.LoadFromFile(kwokKubeconfig)
	if err != nil {
		// Not fatal — kwokctl may have written state differently
		return nil //nolint:nilerr // best-effort rewrite of kwokctl internal state
	}

	if cluster, ok := kwokConfig.Clusters[clusterKey]; ok {
		cluster.Server = newServer
	}

	result, err := clientcmd.Write(*kwokConfig)
	if err != nil {
		return fmt.Errorf("serialize kwokctl kubeconfig: %w", err)
	}

	if err := fsutil.AtomicWriteFile(kwokKubeconfig, result, 0o600); err != nil {
		return fmt.Errorf("write kwokctl kubeconfig: %w", err)
	}

	return nil
}

// kwokStateDir returns the absolute path to kwokctl's cluster state directory.
func kwokStateDir(clusterName string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}

	return filepath.Join(homeDir, ".kwok", "clusters", clusterName), nil
}

// createPlaceholderFiles creates empty placeholder files in the DinD container
// at the exact paths kwokctl will reference in Docker bind mounts.
// This ensures Docker creates file-level bind mounts instead of directory mounts.
func (p *KubernetesProvisioner) createPlaceholderFiles(ctx context.Context, clusterName string) error {
	stateDir, err := kwokStateDir(clusterName)
	if err != nil {
		return err
	}

	pkiDir := filepath.Join(stateDir, "pki")

	// Files kwokctl generates and references in Docker bind mounts
	placeholders := []string{
		filepath.Join(pkiDir, "ca.crt"),
		filepath.Join(pkiDir, "admin.crt"),
		filepath.Join(pkiDir, "admin.key"),
		filepath.Join(stateDir, "kubeconfig"),
		filepath.Join(stateDir, "kwok.yaml"),
	}

	mkdirCmd := fmt.Sprintf("mkdir -p %s", pkiDir)
	if _, err := p.k8sProvider.ExecInDinD(ctx, p.restConfig, p.clusterName, mkdirCmd); err != nil {
		return fmt.Errorf("mkdir pki: %w", err)
	}

	for _, path := range placeholders {
		touchCmd := fmt.Sprintf("touch %s", path)
		if _, err := p.k8sProvider.ExecInDinD(ctx, p.restConfig, p.clusterName, touchCmd); err != nil {
			return fmt.Errorf("touch %s: %w", path, err)
		}
	}

	return nil
}

// copyStateToDinD copies the real PKI files, kubeconfig, and kwok.yaml from
// the local host (where kwokctl generated them) into the DinD container,
// overwriting the placeholders created by createPlaceholderFiles.
func (p *KubernetesProvisioner) copyStateToDinD(ctx context.Context, clusterName string) error {
	stateDir, err := kwokStateDir(clusterName)
	if err != nil {
		return err
	}

	// Files to copy: relative path within stateDir
	files := []string{
		"pki/ca.crt",
		"pki/admin.crt",
		"pki/admin.key",
		"kubeconfig",
		"kwok.yaml",
	}

	for _, rel := range files {
		localPath := filepath.Join(stateDir, rel)

		content, err := os.ReadFile(localPath)
		if err != nil {
			return fmt.Errorf("read local %s: %w", rel, err)
		}

		remotePath := filepath.Join(stateDir, rel)
		if err := p.k8sProvider.WriteFileInDinD(ctx, p.restConfig, p.clusterName, remotePath, string(content)); err != nil {
			return fmt.Errorf("write %s to DinD: %w", rel, err)
		}
	}

	return nil
}

// restartContainersInDinD restarts the crash-looping KWOK Docker containers
// inside DinD so they pick up the real PKI files written by copyStateToDinD.
func (p *KubernetesProvisioner) restartContainersInDinD(ctx context.Context, clusterName string) error {
	containers := kwokContainerNames(clusterName)
	restartCmd := "docker restart " + strings.Join(containers, " ")

	if _, err := p.k8sProvider.ExecInDinD(ctx, p.restConfig, p.clusterName, restartCmd); err != nil {
		return fmt.Errorf("docker restart: %w", err)
	}

	// Give the containers a moment to start up
	time.Sleep(3 * time.Second)

	return nil
}
