package kwokprovisioner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil/scaffolder"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	kubernetesprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/nested"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// kwokContainerRestartDelay is the time to wait after restarting KWOK containers
// so that they can initialize before API server readiness is checked.
const kwokContainerRestartDelay = 3 * time.Second

// KubernetesProvisioner wraps a KWOK provisioner with DinD lifecycle management.
// It uses an exec tunnel for the DinD pod's Docker API, sets DOCKER_HOST so
// kwokctl (docker compose runtime) transparently creates containers inside DinD,
// then port-forwards the nested API server for host access.
type KubernetesProvisioner struct {
	*Provisioner

	k8sProvider      *kubernetesprovider.Provider
	dynamicClient    dynamic.Interface
	restConfig       *rest.Config
	clusterName      string
	distribution     string
	gatewayClassName string
	kubeconfigPath   string
	persistence      v1alpha1.KubernetesPersistence
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
	K8sProvider *kubernetesprovider.Provider
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
	// Persistence holds PVC configuration for the DinD Docker data directory.
	Persistence v1alpha1.KubernetesPersistence
}

// NewKubernetesProvisioner creates a KubernetesProvisioner that wraps KWOK with DinD lifecycle.
func NewKubernetesProvisioner(cfg KubernetesProvisionerConfig) (*KubernetesProvisioner, error) {
	kubeconfigPath, err := k8s.ResolveKubeconfigPath(cfg.KubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("resolve kubeconfig path: %w", err)
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
		persistence:      cfg.Persistence,
	}, nil
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
//
//nolint:cyclop,funlen // Sequential multi-step provisioning flow — extracting steps would obscure the overall sequence.
func (p *KubernetesProvisioner) Create(ctx context.Context, name string) error {
	target := p.resolveName(name)

	// Step 1: Ensure namespace + DinD pod
	err := p.setupDinD(ctx, target)
	if err != nil {
		return err
	}

	// Step 1b: Resolve a stable, server-side exposure (Gateway → LoadBalancer → NodePort) for the
	// nested API server. The API server is pinned to a fixed port (below) so the address can be
	// resolved up-front and added to the API server cert SANs.
	exposure, err := p.k8sProvider.ResolveExposure(
		ctx, p.dynamicClient,
		kubernetesprovider.APIExposureSpec{
			ClusterName:      target,
			APIPort:          kubernetesprovider.DinDAPIServerPort,
			GatewayClassName: p.gatewayClassName,
			HostAddress:      p.restConfig.Host,
		},
	)
	if err != nil {
		return fmt.Errorf("expose API server: %w", err)
	}

	// Step 1c: Pin the API server port and add the exposure address to the API server cert SANs
	// (kwokctl reads this from a generated KwokctlConfiguration), so kubectl verifies TLS when
	// connecting via the stable address.
	cleanupCfg, err := p.applyKwokCertSANs(exposure.Address)
	if err != nil {
		return fmt.Errorf("configure kwok cert SANs: %w", err)
	}
	defer cleanupCfg()

	// Step 2: Pre-create placeholder files in DinD at the exact paths kwokctl
	// will reference in Docker bind mounts. Without this, Docker auto-creates
	// missing bind mount sources as directories, causing kube-apiserver to fail
	// with "read /etc/kubernetes/pki/admin.key: is a directory".
	_, _ = fmt.Fprintln(os.Stdout, "► preparing DinD bind mount paths")

	err = p.createPlaceholderFiles(ctx, target)
	if err != nil {
		return fmt.Errorf("create placeholder files: %w", err)
	}

	// Step 3: Start exec tunnel for Docker API (2375)
	dockerPF, err := p.k8sProvider.StartExecTunnel(
		ctx, p.restConfig, target,
		kubernetesprovider.DinDPodName, kubernetesprovider.DinDContainerName,
		kubernetesprovider.DinDDockerPort,
	)
	if err != nil {
		return fmt.Errorf("exec tunnel Docker API: %w", err)
	}

	defer dockerPF.Close()

	// Step 4: Create the KWOK cluster via kwokctl SDK with DOCKER_HOST → DinD.
	// kwokctl generates PKI locally, writes Docker Compose containers inside DinD.
	// Containers are created with file-level bind mounts (thanks to placeholders)
	// but crash-loop because the placeholder files are empty.
	_, _ = fmt.Fprintln(
		os.Stdout,
		"► creating KWOK cluster via SDK (DOCKER_HOST → exec tunnel → DinD)",
	)

	err = kubernetesprovider.WithRemoteDockerHost(dockerPF, func() error {
		return p.CreateCluster(ctx, name)
	})
	if err != nil {
		return fmt.Errorf("kwok create via SDK: %w", err)
	}

	// Step 5: Copy real PKI, kubeconfig, and kwok.yaml from the local host
	// (where kwokctl wrote them) into DinD, overwriting the placeholders.
	_, _ = fmt.Fprintln(os.Stdout, "► syncing PKI and config into DinD")

	err = p.copyStateToDinD(ctx, target)
	if err != nil {
		return fmt.Errorf("copy state to DinD: %w", err)
	}

	// Step 6: Restart crash-looping containers so they pick up the real files.
	// Bind mounts reference the DinD filesystem; a restart rereads the content.
	_, _ = fmt.Fprintln(os.Stdout, "► restarting KWOK containers in DinD")

	err = p.restartContainersInDinD(ctx, target)
	if err != nil {
		return fmt.Errorf("restart containers: %w", err)
	}

	// Step 7: Discover the API server port from the kubeconfig that kwokctl wrote.
	apiServerPort, err := p.discoverAPIServerPort(name)
	if err != nil {
		return fmt.Errorf("discover API server port: %w", err)
	}

	// Step 8: Port-forward the nested API server from DinD to localhost. This forward is
	// transient — it is only used for the readiness check and node scaling below, and is closed
	// before Create returns. Steady-state access goes through the stable exposure address.
	_, _ = fmt.Fprintln(os.Stdout, "► port-forwarding nested API server to localhost")

	portForward, err := p.k8sProvider.StartPortForward(
		ctx, p.restConfig, target,
		kubernetesprovider.DinDPodName, apiServerPort,
	)
	if err != nil {
		return fmt.Errorf("port-forward API server: %w", err)
	}

	defer portForward.Close()

	// Step 9: Temporarily point the kubeconfig at the loopback port-forward so the readiness
	// check and node scaling can reach the API during creation (the cert includes 127.0.0.1).
	err = p.rewriteKubeconfig(name, fmt.Sprintf("https://127.0.0.1:%d", portForward.LocalPort))
	if err != nil {
		return fmt.Errorf("rewrite kubeconfig: %w", err)
	}

	// Step 10: Wait for the nested API server to be reachable through port-forward.
	_, _ = fmt.Fprintln(os.Stdout, "► waiting for nested API server to be ready")

	err = k8s.WaitForAPIServer(ctx, p.kubeconfigPath, "kwok-"+target)
	if err != nil {
		return fmt.Errorf("wait for API server: %w", err)
	}

	// Step 11: Scale simulated nodes — now the API server is accessible via port-forward
	_, _ = fmt.Fprintln(os.Stdout, "► creating simulated KWOK node")

	err = p.ScaleNodes(ctx, name)
	if err != nil {
		return fmt.Errorf("kwok scale via SDK: %w", err)
	}

	// Step 12: Point the kubeconfig at the stable exposure address (survives the CLI process exit).
	err = p.rewriteKubeconfig(name, exposure.ServerURL())
	if err != nil {
		return fmt.Errorf("rewrite kubeconfig: %w", err)
	}

	return nil
}

// Delete deletes the KWOK cluster inside DinD and cleans up host cluster resources.
// The inner kwokctl SDK delete uses the raw name (unlike Kind, which uses the
// resolved target).
func (p *KubernetesProvisioner) Delete(ctx context.Context, name string) error {
	target := p.resolveName(name)

	//nolint:wrapcheck // DinDLifecycle.Delete already wraps with teardown/cleanup context
	return p.dindLifecycle().Delete(ctx, target, "kwok-"+target, func() error {
		return p.Provisioner.Delete(ctx, name)
	})
}

// These thin per-distribution wrappers delegate to the shared nested.DinDLifecycle;
// they are structurally identical to the Kind sibling by design (only the name
// resolution differs) and are intentionally not abstracted further.
// jscpd:ignore-start

// Exists checks if the KWOK-on-Kubernetes cluster exists by checking for the DinD pod.
func (p *KubernetesProvisioner) Exists(ctx context.Context, name string) (bool, error) {
	target := p.resolveName(name)

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
// KWOK-on-Kubernetes provisioner.
func (p *KubernetesProvisioner) dindLifecycle() nested.DinDLifecycle {
	return nested.DinDLifecycle{
		Provider:       p.k8sProvider,
		DynamicClient:  p.dynamicClient,
		RestConfig:     p.restConfig,
		KubeconfigPath: p.kubeconfigPath,
		LogWriter:      os.Stdout,
	}
}

// jscpd:ignore-end

// discoverAPIServerPort reads the kubeconfig that kwokctl wrote and extracts
// the API server port from the cluster entry. kwokctl (docker compose runtime)
// maps the API server's internal port 6443 to a random host port inside DinD
// and writes that random port into the kubeconfig.
func (p *KubernetesProvisioner) discoverAPIServerPort(name string) (int, error) {
	config, err := clientcmd.LoadFromFile(p.kubeconfigPath)
	if err != nil {
		return 0, fmt.Errorf("load kubeconfig: %w", err)
	}

	target := p.resolveName(name)
	clusterKey := "kwok-" + target

	cluster, ok := config.Clusters[clusterKey]
	if !ok {
		return 0, fmt.Errorf("%w: %s", k8s.ErrClusterEntryNotFound, clusterKey)
	}

	// Server URL is typically https://127.0.0.1:<port>
	var port int

	_, err = fmt.Sscanf(cluster.Server, "https://127.0.0.1:%d", &port)
	if err != nil {
		return 0, fmt.Errorf("parse API server port from %q: %w", cluster.Server, err)
	}

	return port, nil
}

// rewriteKubeconfig rewrites the KWOK kubeconfig server URL to the given URL. Updates both:
//   - ~/.kube/config — the standard kubeconfig for kubectl access
//   - ~/.kwok/clusters/<name>/kubeconfig.yaml — kwokctl's internal kubeconfig
//     used by kwokctl scale and other runtime commands
func (p *KubernetesProvisioner) rewriteKubeconfig(name, newServer string) error {
	target := p.resolveName(name)
	clusterKey := "kwok-" + target

	// Rewrite ~/.kube/config (atomic, merge-safe)
	err := k8s.ModifyKubeconfigCluster(p.kubeconfigPath, clusterKey, newServer)
	if err != nil {
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

	kwokKubeconfigPath := kwokKubeconfig

	err = fsutil.AtomicWriteFile(
		kwokKubeconfigPath,
		result,
		0o600, //nolint:mnd // standard kubeconfig file permission
	)
	if err != nil {
		return fmt.Errorf("write kwokctl kubeconfig: %w", err)
	}

	return nil
}

// applyKwokCertSANs writes a temporary kwok config directory that pins the API server port and
// adds the exposure address to the API server certificate SANs, then points the inner kwokctl
// provisioner at it. It returns a cleanup function that removes the temporary directory.
//
// For the Kubernetes provider this overrides any user-supplied kwok config path; the default
// node-simulation config is reproduced so node scaling continues to work.
func (p *KubernetesProvisioner) applyKwokCertSANs(address string) (func(), error) {
	noop := func() {}

	dir, err := os.MkdirTemp("", "kwok-k8s-*")
	if err != nil {
		return noop, fmt.Errorf("create temp config dir: %w", err)
	}

	cleanup := func() { _ = os.RemoveAll(dir) }

	kustomization := "apiVersion: kustomize.config.k8s.io/v1beta1\n" +
		"kind: Kustomization\n" +
		"resources:\n" +
		"  - simulation.yaml\n" +
		"  - kwokctl.yaml\n"

	kwokctl := fmt.Sprintf(`apiVersion: config.kwok.x-k8s.io/v1alpha1
kind: KwokctlConfiguration
metadata:
  name: kwok
options:
  kubeApiserverPort: %d
  kubeApiserverCertSANs:
  - "127.0.0.1"
  - "localhost"
  - %q
`, kubernetesprovider.DinDAPIServerPort, address)

	files := map[string]string{
		"kustomization.yaml": kustomization,
		"simulation.yaml":    scaffolder.KWOKDefaultSimulationConfig,
		"kwokctl.yaml":       kwokctl,
	}

	const fileMode = 0o600

	for name, content := range files {
		writeErr := os.WriteFile(filepath.Join(dir, name), []byte(content), fileMode)
		if writeErr != nil {
			cleanup()

			return noop, fmt.Errorf("write %s: %w", name, writeErr)
		}
	}

	originalConfigPath := p.configPath
	p.configPath = dir

	return func() {
		p.configPath = originalConfigPath
		_ = os.RemoveAll(dir)
	}, nil
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
func (p *KubernetesProvisioner) createPlaceholderFiles(
	ctx context.Context,
	clusterName string,
) error {
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

	mkdirCmd := fmt.Sprintf("mkdir -p '%s'", strings.ReplaceAll(pkiDir, "'", "'\\''"))

	_, err = p.k8sProvider.ExecInDinD(ctx, p.restConfig, clusterName, mkdirCmd)
	if err != nil {
		return fmt.Errorf("mkdir pki: %w", err)
	}

	for _, path := range placeholders {
		escapedPath := strings.ReplaceAll(path, "'", "'\\''")

		touchCmd := fmt.Sprintf("touch '%s'", escapedPath)

		_, err := p.k8sProvider.ExecInDinD(
			ctx,
			p.restConfig,
			clusterName,
			touchCmd,
		)
		if err != nil {
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

		//nolint:gosec // path constructed from trusted state directory
		content, err := os.ReadFile(
			localPath,
		)
		if err != nil {
			return fmt.Errorf("read local %s: %w", rel, err)
		}

		remotePath := filepath.Join(stateDir, rel)

		err = p.k8sProvider.WriteFileInDinD(
			ctx,
			p.restConfig,
			clusterName,
			remotePath,
			string(content),
		)
		if err != nil {
			return fmt.Errorf("write %s to DinD: %w", rel, err)
		}
	}

	return nil
}

// restartContainersInDinD restarts the crash-looping KWOK Docker containers
// inside DinD so they pick up the real PKI files written by copyStateToDinD.
func (p *KubernetesProvisioner) restartContainersInDinD(
	ctx context.Context,
	clusterName string,
) error {
	containers := kwokContainerNames(clusterName)
	restartCmd := "docker restart " + strings.Join(containers, " ")

	_, err := p.k8sProvider.ExecInDinD(ctx, p.restConfig, clusterName, restartCmd)
	if err != nil {
		return fmt.Errorf("docker restart: %w", err)
	}

	// Give the containers a moment to start up
	time.Sleep(kwokContainerRestartDelay)

	return nil
}
