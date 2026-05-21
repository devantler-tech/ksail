package talosprovisioner

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	kubernetesprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kernelmod"
	dockerclient "github.com/docker/docker/client"
	"github.com/siderolabs/talos/pkg/provision"
	"github.com/siderolabs/talos/pkg/provision/access"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

// KubernetesProvisioner wraps a Talos provisioner with DinD lifecycle management.
// It port-forwards the DinD pod's Docker API to localhost, sets DOCKER_HOST so
// the Talos SDK transparently creates containers inside DinD, then port-forwards
// the Talos API and K8s API for host access.
//
// Because this type is in the same package as Provisioner, it can call unexported
// methods (provisionCluster, getMappedTalosAPIEndpoint, etc.) to implement the
// two-phase create flow needed for DinD.
type KubernetesProvisioner struct {
	inner            *Provisioner
	k8sProvider      *kubernetesprovider.Provider
	dynamicClient    dynamic.Interface
	restConfig       *rest.Config
	clusterName      string
	distribution     string
	gatewayClassName string
	kubeconfigPath   string
	persistence      v1alpha1.KubernetesPersistence
	portForwards     []*kubernetesprovider.PortForwardSession
}

// KubernetesProvisionerConfig holds configuration for creating a KubernetesProvisioner.
type KubernetesProvisionerConfig struct {
	// InnerProvisioner is the fully-configured Talos Provisioner (Docker provider type).
	InnerProvisioner *Provisioner
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
	// ControlPlanes is the number of control-plane nodes.
	ControlPlanes int
	// Workers is the number of worker nodes.
	Workers int
	// Persistence holds PVC configuration for the DinD Docker data directory.
	Persistence v1alpha1.KubernetesPersistence
}

// NewKubernetesProvisioner creates a KubernetesProvisioner that wraps Talos with DinD lifecycle.
func NewKubernetesProvisioner(cfg KubernetesProvisionerConfig) (*KubernetesProvisioner, error) {
	kubeconfigPath, err := k8s.ResolveKubeconfigPath(cfg.KubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("resolve kubeconfig path: %w", err)
	}

	return &KubernetesProvisioner{
		inner:            cfg.InnerProvisioner,
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

// Create creates a Talos cluster inside a DinD pod on the host Kubernetes cluster.
//
// Two-phase flow:
//  1. Port-forward Docker API → set DOCKER_HOST → provision containers inside DinD
//  2. Discover DinD-internal mapped ports → port-forward Talos API + K8s API → bootstrap
//
// would obscure the overall sequence.
//
//nolint:cyclop,funlen // Sequential multi-phase provisioning flow — extracting phases
func (p *KubernetesProvisioner) Create(ctx context.Context, name string) error {
	clusterName := name
	if clusterName == "" {
		clusterName = p.clusterName
	}

	// jscpd:ignore-start
	// Preserve the host kubeconfig's current-context. MergeKubeconfig overwrites
	// current-context with the nested cluster's context, which would cause subsequent
	// Kubernetes provider operations (info, delete) to connect to the nested cluster
	// instead of the host cluster.
	originalContext, err := k8s.GetKubeconfigCurrentContext(p.kubeconfigPath)
	if err != nil {
		return fmt.Errorf("read current kubeconfig context: %w", err)
	}

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
	// jscpd:ignore-end

	// Step 1: Ensure namespace + DinD pod
	err = p.setupDinD(ctx, clusterName)
	if err != nil {
		return err
	}

	// Step 1b: Resolve a stable, server-side exposure (Gateway → LoadBalancer → NodePort) for the
	// nested Kubernetes API server. The Service is created up-front (its target port is corrected
	// after the DinD-mapped K8s port is discovered) so the address can be added to the cert SANs.
	exposure, err := p.k8sProvider.ResolveExposure(
		ctx, p.dynamicClient,
		kubernetesprovider.APIExposureSpec{
			ClusterName:      clusterName,
			APIPort:          kubernetesprovider.DinDAPIServerPort,
			GatewayClassName: p.gatewayClassName,
			HostAddress:      p.restConfig.Host,
			SkipLoadBalancer: true,
		},
	)
	if err != nil {
		return fmt.Errorf("expose API server: %w", err)
	}

	// Step 1c: Regenerate the Talos config so the API server cert is valid for the exposure
	// address (keeping loopback for the creation-time port-forward), preserving the PKI.
	newConfigs, err := p.inner.talosConfigs.WithCertSANs(
		[]string{"127.0.0.1", "localhost", exposure.Address},
	)
	if err != nil {
		return fmt.Errorf("add exposure address to cert SANs: %w", err)
	}

	p.inner.talosConfigs = newConfigs

	// Step 2: Start exec tunnel for Docker API (2375) to localhost.
	// The exec tunnel uses CRI exec + nc instead of SPDY port-forward,
	// which correctly handles Docker's HTTP connection hijacking (101 Upgrade)
	// for docker exec operations.
	dockerPF, err := p.k8sProvider.StartExecTunnel(
		ctx, p.restConfig, clusterName,
		kubernetesprovider.DinDPodName, kubernetesprovider.DinDContainerName,
		kubernetesprovider.DinDDockerPort,
	)
	if err != nil {
		return fmt.Errorf("port-forward Docker API: %w", err)
	}

	p.portForwards = append(p.portForwards, dockerPF)

	// Step 3: Set DOCKER_HOST and create a Docker client for DinD
	dockerHost := fmt.Sprintf("tcp://127.0.0.1:%d", dockerPF.LocalPort)

	// jscpd:ignore-start
	_, _ = fmt.Fprintf(os.Stdout, "► creating Talos cluster via SDK (DOCKER_HOST=%s)\n", dockerHost)

	origHost := os.Getenv("DOCKER_HOST")

	err = os.Setenv("DOCKER_HOST", dockerHost)
	if err != nil {
		return fmt.Errorf("set DOCKER_HOST: %w", err)
	}

	defer func() {
		if origHost == "" {
			_ = os.Unsetenv("DOCKER_HOST")
		} else {
			_ = os.Setenv("DOCKER_HOST", origHost)
		}
	}()

	// Create a Docker client pointing at DinD and inject it into the inner provisioner
	// jscpd:ignore-end
	dindClient, err := dockerclient.NewClientWithOpts(
		dockerclient.FromEnv,
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return fmt.Errorf("create DinD Docker client: %w", err)
	}

	defer func() { _ = dindClient.Close() }()

	p.inner.WithDockerClient(dindClient)

	err = kernelmod.EnsureBrNetfilter(ctx, p.inner.logWriter)
	if err != nil {
		return fmt.Errorf("kernel module check: %w", err)
	}

	err = p.inner.checkDockerAvailable(ctx)
	if err != nil {
		return fmt.Errorf("DinD Docker not available: %w", err)
	}

	err = p.inner.ensureTalosImage(ctx)
	if err != nil {
		return fmt.Errorf("ensure Talos image: %w", err)
	}

	configBundle := p.inner.talosConfigs.Bundle()

	provisionStart := time.Now()

	cluster, err := p.inner.provisionCluster(ctx, clusterName, configBundle)
	if err != nil {
		return fmt.Errorf("provision cluster: %w", err)
	}

	_, _ = fmt.Fprintf(os.Stdout, "► containers provisioned inside DinD [%s]\n",
		time.Since(provisionStart).Truncate(time.Second))

	// === Phase 2: Port-forward Talos API + K8s API from DinD, then bootstrap ===

	// Discover mapped Talos API port inside DinD (e.g., 127.0.0.1:32001)
	talosEndpoint, err := p.inner.getMappedTalosAPIEndpoint(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("get Talos API endpoint: %w", err)
	}

	talosPort, err := parsePort(talosEndpoint)
	if err != nil {
		return fmt.Errorf("parse Talos API port: %w", err)
	}

	// Discover mapped K8s API port inside DinD
	k8sEndpoint, err := p.inner.getMappedK8sAPIEndpoint(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("get K8s API endpoint: %w", err)
	}

	k8sPort, err := parsePort(k8sEndpoint)
	if err != nil {
		return fmt.Errorf("parse K8s API port: %w", err)
	}

	// Port-forward Talos API from DinD to host
	talosPF, err := p.k8sProvider.StartPortForward(
		ctx, p.restConfig, clusterName,
		kubernetesprovider.DinDPodName, talosPort,
	)
	if err != nil {
		return fmt.Errorf("port-forward Talos API: %w", err)
	}

	p.portForwards = append(p.portForwards, talosPF)

	// Port-forward K8s API from DinD to host
	k8sPF, err := p.k8sProvider.StartPortForward(
		ctx, p.restConfig, clusterName,
		kubernetesprovider.DinDPodName, k8sPort,
	)
	if err != nil {
		return fmt.Errorf("port-forward K8s API: %w", err)
	}

	p.portForwards = append(p.portForwards, k8sPF)

	hostTalosEndpoint := net.JoinHostPort("127.0.0.1", strconv.Itoa(talosPF.LocalPort))
	hostK8sEndpoint := fmt.Sprintf("https://127.0.0.1:%d", k8sPF.LocalPort)

	_, _ = fmt.Fprintf(
		os.Stdout,
		"► Talos API: %s → localhost:%d\n",
		talosEndpoint,
		talosPF.LocalPort,
	)
	_, _ = fmt.Fprintf(os.Stdout, "► K8s API: %s → localhost:%d\n", k8sEndpoint, k8sPF.LocalPort)

	// Save talosconfig with host-accessible endpoint
	talosConfig := configBundle.TalosConfig()
	patchTalosConfigEndpoint(talosConfig, hostTalosEndpoint)

	if p.inner.options.TalosconfigPath != "" {
		saveErr := p.inner.saveTalosconfig(configBundle)
		if saveErr != nil {
			return fmt.Errorf("save talosconfig: %w", saveErr)
		}
	}

	// Create cluster access with host-accessible endpoints
	clusterAccess := access.NewAdapter(
		cluster,
		provision.WithTalosConfig(talosConfig),
		provision.WithKubernetesEndpoint(hostK8sEndpoint),
	)

	defer func() { _ = clusterAccess.Close() }()

	// Bootstrap and wait for readiness
	err = p.inner.bootstrapAndWaitForReady(ctx, clusterAccess)
	if err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}

	// Fetch and save kubeconfig (server set to the loopback endpoint used for bootstrap).
	err = p.inner.fetchAndSaveKubeconfig(ctx, clusterAccess)
	if err != nil {
		return fmt.Errorf("save kubeconfig: %w", err)
	}

	// Point the exposure Service at the DinD-mapped K8s API port (only known now).
	//nolint:gosec // port value is bounded within TCP port range (1-65535)
	err = p.k8sProvider.UpdateAPIServiceTargetPort(ctx, clusterName, int32(k8sPort))
	if err != nil {
		return fmt.Errorf("update API exposure target port: %w", err)
	}

	// Repoint the persisted kubeconfig at the stable exposure address (survives the CLI exit).
	err = k8s.ModifyKubeconfigCluster(p.kubeconfigPath, clusterName, exposure.ServerURL())
	if err != nil {
		return fmt.Errorf("repoint kubeconfig at exposure address: %w", err)
	}

	return nil
}

// Delete deletes the Talos cluster inside DinD and cleans up host cluster resources.
func (p *KubernetesProvisioner) Delete(ctx context.Context, name string) error {
	clusterName := name
	if clusterName == "" {
		clusterName = p.clusterName
	}

	// Close all port-forwards
	for _, pf := range p.portForwards {
		pf.Close()
	}

	p.portForwards = nil

	// jscpd:ignore-start
	// Best-effort: delete Talos cluster inside DinD via SDK
	dockerPF, pfErr := p.k8sProvider.StartExecTunnel(
		ctx, p.restConfig, clusterName,
		kubernetesprovider.DinDPodName, kubernetesprovider.DinDContainerName,
		kubernetesprovider.DinDDockerPort,
	)
	if pfErr == nil {
		defer dockerPF.Close()

		_ = kubernetesprovider.WithRemoteDockerHost(dockerPF, func() error {
			dindClient, clientErr := dockerclient.NewClientWithOpts(
				dockerclient.FromEnv,
				dockerclient.WithAPIVersionNegotiation(),
			)
			if clientErr != nil {
				return fmt.Errorf("create Docker client: %w", clientErr)
			}

			defer func() { _ = dindClient.Close() }()

			p.inner.WithDockerClient(dindClient)

			return p.inner.deleteDockerCluster(ctx, clusterName)
		})
	}

	// Clean up API exposure, DinD, and namespace
	err := p.k8sProvider.TeardownDinD(ctx, p.dynamicClient, clusterName)
	if err != nil {
		return fmt.Errorf("teardown DinD: %w", err)
	}

	return nil
}

// Exists checks if the Talos cluster exists by checking for the DinD pod.
func (p *KubernetesProvisioner) Exists(ctx context.Context, name string) (bool, error) {
	clusterName := name
	if clusterName == "" {
		clusterName = p.clusterName
	}

	exists, err := p.k8sProvider.NodesExist(ctx, clusterName)
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

// Start is not supported for Talos-on-Kubernetes.
func (p *KubernetesProvisioner) Start(_ context.Context, _ string) error {
	return ErrStartNotSupported
}

// Stop is not supported for Talos-on-Kubernetes.
func (p *KubernetesProvisioner) Stop(_ context.Context, _ string) error {
	return ErrStopNotSupported
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

// parsePort extracts the port number from a "host:port" endpoint string.
// Returns a value in the valid TCP port range [1, 65535].
func parsePort(endpoint string) (int, error) {
	_, portStr, err := net.SplitHostPort(endpoint)
	if err != nil {
		return 0, fmt.Errorf("split host:port %q: %w", endpoint, err)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0, fmt.Errorf("parse port %q: %w", portStr, err)
	}

	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("%w: %d", ErrInvalidPort, port)
	}

	return port, nil
}
