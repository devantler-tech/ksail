package talosprovisioner

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	kubernetesprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
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
	hostContext      string
	kubeconfigPath   string
	persistence      v1alpha1.KubernetesPersistence
	mirrorSpecs      []registry.MirrorSpec
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
	// HostContext is the explicitly-configured host kubeconfig context (empty = resolved from
	// the kubeconfig's current-context).
	HostContext string
	// ControlPlanes is the number of control-plane nodes.
	ControlPlanes int
	// Workers is the number of worker nodes.
	Workers int
	// Persistence holds PVC configuration for the DinD Docker data directory.
	Persistence v1alpha1.KubernetesPersistence
	// MirrorSpecs configures pull-through registry mirrors set up inside the DinD env
	// so the nested cluster pulls images through authenticated, caching registries
	// (avoiding anonymous upstream rate limits). Empty disables nested mirroring.
	MirrorSpecs []registry.MirrorSpec
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
		hostContext:      cfg.HostContext,
		kubeconfigPath:   kubeconfigPath,
		persistence:      cfg.Persistence,
		mirrorSpecs:      cfg.MirrorSpecs,
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

	// Preserve the host kubeconfig's current-context (which MergeKubeconfig would otherwise
	// overwrite with the nested cluster) when the host is resolved from current-context. With an
	// explicit host context configured, leave the user pointed at the new nested cluster.
	restoreContext, err := k8s.PreserveCurrentContextUnlessExplicit(p.kubeconfigPath, p.hostContext)
	if err != nil {
		return fmt.Errorf("preserve host kubeconfig context: %w", err)
	}

	defer restoreContext()

	// Step 1: Ensure namespace + DinD pod
	err = p.setupDinD(ctx, clusterName)
	if err != nil {
		return err
	}

	// Step 1b: Resolve a stable, server-side exposure and bake its address into the API server
	// cert SANs (the Service target port is corrected once the DinD-mapped K8s port is known).
	exposure, err := p.prepareExposure(ctx, clusterName)
	if err != nil {
		return err
	}

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

	err = p.inner.kernelModuleLoader(ctx, p.inner.logWriter)
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

	// Set up authenticated pull-through registry mirrors inside DinD so the nested
	// cluster pulls k8s/control-plane images through them (avoiding anonymous upstream
	// rate limits that otherwise stall the kubelet). Must run before Bundle() so the
	// mirror config is baked into the machine config the nodes boot with.
	err = p.setupDinDMirrors(ctx, dindClient, clusterName)
	if err != nil {
		return fmt.Errorf("set up nested registry mirrors: %w", err)
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

	// Discover the host ports the Talos API and K8s API are mapped to inside DinD.
	talosPort, k8sPort, err := p.discoverMappedPorts(ctx, clusterName)
	if err != nil {
		return err
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

	_, _ = fmt.Fprintf(os.Stdout, "► Talos API → localhost:%d\n", talosPF.LocalPort)
	_, _ = fmt.Fprintf(os.Stdout, "► K8s API → localhost:%d\n", k8sPF.LocalPort)

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

// prepareExposure resolves a stable, server-side exposure for the nested Kubernetes API server and
// regenerates the Talos config so the API server certificate is valid for that address (preserving
// the existing PKI). The Service target port is corrected later, once the DinD-mapped K8s port is
// known.
func (p *KubernetesProvisioner) prepareExposure(
	ctx context.Context,
	clusterName string,
) (*kubernetesprovider.ExposureResult, error) {
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
		return nil, fmt.Errorf("expose API server: %w", err)
	}

	newConfigs, err := p.inner.talosConfigs.WithCertSANs(
		[]string{"127.0.0.1", "localhost", exposure.Address},
	)
	if err != nil {
		return nil, fmt.Errorf("add exposure address to cert SANs: %w", err)
	}

	p.inner.talosConfigs = newConfigs

	return exposure, nil
}

// discoverMappedPorts returns the host ports the Talos API and Kubernetes API are published on
// inside the DinD container, in that order (talosPort, k8sPort).
func (p *KubernetesProvisioner) discoverMappedPorts(
	ctx context.Context,
	clusterName string,
) (int, int, error) {
	talosEndpoint, err := p.inner.getMappedTalosAPIEndpoint(ctx, clusterName)
	if err != nil {
		return 0, 0, fmt.Errorf("get Talos API endpoint: %w", err)
	}

	talosPort, err := parsePort(talosEndpoint)
	if err != nil {
		return 0, 0, fmt.Errorf("parse Talos API port: %w", err)
	}

	k8sEndpoint, err := p.inner.getMappedK8sAPIEndpoint(ctx, clusterName)
	if err != nil {
		return 0, 0, fmt.Errorf("get K8s API endpoint: %w", err)
	}

	k8sPort, err := parsePort(k8sEndpoint)
	if err != nil {
		return 0, 0, fmt.Errorf("parse K8s API port: %w", err)
	}

	return talosPort, k8sPort, nil
}

// setupDinDMirrors stands up authenticated pull-through registry mirrors inside the
// DinD Docker daemon, on the nested cluster's Docker network, and points the nested
// Talos machine config at them. This mirrors what the Docker provider does on the host
// for standalone clusters, so the nested cluster's containerd pulls images through
// caching, credentialed registries instead of anonymously from upstream. No-op when no
// mirror specs are configured.
func (p *KubernetesProvisioner) setupDinDMirrors(
	ctx context.Context,
	dindClient dockerclient.APIClient,
	clusterName string,
) error {
	if len(p.mirrorSpecs) == 0 {
		return nil
	}

	writer := p.inner.logWriter

	backend, err := registry.DefaultBackendFactory(dindClient)
	if err != nil {
		return fmt.Errorf("create nested registry backend: %w", err)
	}

	usedPorts, err := registry.CollectExistingRegistryPorts(ctx, backend)
	if err != nil {
		return fmt.Errorf("collect nested registry ports: %w", err)
	}

	upstreams := registry.BuildUpstreamLookup(p.mirrorSpecs)
	registryInfos := registry.BuildRegistryInfosFromSpecs(
		p.mirrorSpecs, upstreams, usedPorts, clusterName,
	)

	if len(registryInfos) == 0 {
		return nil
	}

	// Create the pull-through registry containers inside DinD.
	err = registry.SetupRegistries(ctx, backend, registryInfos, clusterName, "", writer)
	if err != nil {
		return fmt.Errorf("create nested mirror registries: %w", err)
	}

	// Pre-create the cluster network with the CIDR the Talos SDK uses for its Docker
	// bridge (not the pod CIDR), so the SDK reuses it (same name/CIDR) when provisioning
	// the nodes instead of creating a conflicting one.
	networkCIDR := talosconfigmanager.DefaultNetworkCIDR

	err = registry.EnsureNetwork(ctx, dindClient, clusterName, networkCIDR, writer)
	if err != nil {
		return fmt.Errorf("ensure nested cluster network: %w", err)
	}

	// Connect the registries with static IPs from the high end of the subnet so they
	// don't claim the low addresses the Talos SDK assigns to nodes (e.g. 10.5.0.2),
	// while remaining resolvable by container name via Docker DNS.
	_, err = registry.ConnectRegistriesToNetworkWithStaticIPs(
		ctx, dindClient, registryInfos, clusterName, networkCIDR, writer,
	)
	if err != nil {
		return fmt.Errorf("connect nested mirror registries to network: %w", err)
	}

	p.applyMirrorsToTalosConfig(clusterName)

	return nil
}

// applyMirrorsToTalosConfig points the nested Talos machine config's
// registries.mirrors at the in-DinD registry containers (by Docker DNS name).
func (p *KubernetesProvisioner) applyMirrorsToTalosConfig(clusterName string) {
	mirrors := make([]talosconfigmanager.MirrorRegistry, 0, len(p.mirrorSpecs))

	for _, spec := range p.mirrorSpecs {
		containerName := registry.BuildRegistryName(clusterName, spec.Host)
		username, password := spec.ResolveCredentials()

		mirrors = append(mirrors, talosconfigmanager.MirrorRegistry{
			Host:      spec.Host,
			Endpoints: []string{"http://" + containerName + ":5000"},
			Username:  username,
			Password:  password,
		})
	}

	_ = p.inner.talosConfigs.ApplyMirrorRegistries(mirrors)
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
