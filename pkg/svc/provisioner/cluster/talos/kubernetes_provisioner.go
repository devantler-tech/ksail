package talosprovisioner

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	dockerpkg "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	kubernetesprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/nested"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
	"github.com/docker/docker/api/types/container"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/siderolabs/talos/pkg/provision"
	"github.com/siderolabs/talos/pkg/provision/access"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
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
	hostClientset    kubernetes.Interface
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
	// HostClientset is the typed client for the host cluster, used to publish the nested
	// cluster's kubeconfig Secret for the Connector contract.
	HostClientset kubernetes.Interface
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
		hostClientset:    cfg.HostClientset,
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

	// Step 3: Point DOCKER_HOST at the DinD daemon for the remainder of Create (the
	// Talos SDK reads DOCKER_HOST across the sequential provisioning steps below, so
	// the env var stays set until Create returns) and create a Docker client for DinD.
	_, _ = fmt.Fprintf(
		os.Stdout,
		"► creating Talos cluster via SDK (DOCKER_HOST=tcp://127.0.0.1:%d)\n",
		dockerPF.LocalPort,
	)

	restoreDockerHost, err := nested.SetDockerHost(dockerPF.LocalPort)
	if err != nil {
		return err //nolint:wrapcheck // helper already wraps with "set DOCKER_HOST" context
	}

	defer restoreDockerHost()

	// Create a Docker client pointing at DinD and inject it into the inner provisioner
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
		// Opt-in (KSAIL_NESTED_DEBUG) diagnostic: dump nested Talos node + mirror
		// container logs to understand why the kubelet stalls in "Preparing" (image
		// pull) — is it using the mirror, is the upstream pull failing/auth-falling-
		// back, or just slow on a cold cache? No-op unless the env var is set.
		p.dumpNestedDiagnostics(ctx, dindClient, clusterName)

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

	// Publish the kubeconfig as an in-cluster Secret so the operator (Connector contract) can
	// reach the nested cluster without a DinD-internal or host-only address.
	raw, err := clusterAccess.Kubeconfig(ctx)
	if err != nil {
		return fmt.Errorf("fetch kubeconfig for connector publish: %w", err)
	}

	err = p.publishConnectorKubeconfig(ctx, clusterName, raw)
	if err != nil {
		return fmt.Errorf("publish connector kubeconfig: %w", err)
	}

	return nil
}

// Kubeconfig returns a kubeconfig for the named nested Talos cluster reachable from inside the
// host cluster (where the operator runs). Create publishes it with the API server already
// rewritten to the in-cluster apiserver Service endpoint (see publishConnectorKubeconfig), so it
// is served as-published. It satisfies the clusterprovisioner.Connector capability and returns
// clustererr.ErrKubeconfigNotReady while the talos-<name>-kubeconfig Secret has not been
// published yet.
func (p *KubernetesProvisioner) Kubeconfig(ctx context.Context, name string) ([]byte, error) {
	clusterName := name
	if clusterName == "" {
		clusterName = p.clusterName
	}

	conn := ConnectionFor(clusterName)

	raw, err := nested.ConnectorKubeconfig(
		ctx, p.hostClientset, clusterName,
		conn.Namespace, conn.SecretName, talosKubeconfigKey,
	)
	if err != nil {
		return nil, fmt.Errorf("talos: %w", err)
	}

	return raw, nil
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

	// The in-DinD delete needs a Docker client pointed at the DinD daemon, created
	// inside the DOCKER_HOST scope the shared helper establishes.
	//nolint:wrapcheck // DinDLifecycle.Delete already wraps with "teardown DinD" context
	return p.dindLifecycle().Delete(ctx, clusterName, "", func() error {
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

// Exists checks if the Talos cluster exists by checking for the DinD pod.
func (p *KubernetesProvisioner) Exists(ctx context.Context, name string) (bool, error) {
	clusterName := name
	if clusterName == "" {
		clusterName = p.clusterName
	}

	//nolint:wrapcheck // DinDLifecycle.Exists already wraps with "check nodes" context
	return p.dindLifecycle().Exists(ctx, clusterName)
}

// List returns cluster names found by namespace.
func (p *KubernetesProvisioner) List(ctx context.Context) ([]string, error) {
	//nolint:wrapcheck // DinDLifecycle.List already wraps with "list clusters" context
	return p.dindLifecycle().List(ctx)
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

	// The in-cluster Service DNS names (Connection.CertSANs) are included so the kubeconfig
	// published for the Connector contract verifies against the served certificate.
	newConfigs, err := p.inner.talosConfigs.WithCertSANs(
		append(
			[]string{"127.0.0.1", "localhost", exposure.Address},
			ConnectionFor(clusterName).CertSANs()...,
		),
	)
	if err != nil {
		return nil, fmt.Errorf("add exposure address to cert SANs: %w", err)
	}

	p.inner.talosConfigs = newConfigs

	return exposure, nil
}

// publishConnectorKubeconfig publishes the nested cluster's kubeconfig in the exposure namespace
// under the Connection naming contract, with every server rewritten to the in-cluster apiserver
// Service endpoint (a cert SAN since prepareExposure — never a DinD-internal address). Kubeconfig()
// serves this Secret back to the operator as-published.
func (p *KubernetesProvisioner) publishConnectorKubeconfig(
	ctx context.Context,
	clusterName string,
	raw []byte,
) error {
	if p.hostClientset == nil {
		return fmt.Errorf("%w: host clientset not set", clustererr.ErrConfigNil)
	}

	conn := ConnectionFor(clusterName)

	rewritten, err := rewriteKubeconfigEndpoint(raw, conn.Endpoint)
	if err != nil {
		return fmt.Errorf("rewrite kubeconfig to in-cluster endpoint: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      conn.SecretName,
			Namespace: conn.Namespace,
			Labels:    kubernetesprovider.CommonLabels(clusterName),
		},
		Data: map[string][]byte{talosKubeconfigKey: rewritten},
	}

	err = nested.UpsertSecret(ctx, p.hostClientset, secret)
	if err != nil {
		return fmt.Errorf("write kubeconfig secret: %w", err)
	}

	return nil
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
	dindClient dockerpkg.Client,
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

	// Use the nested (DinD) MTU so registry/upstream and node↔mirror traffic fits the
	// DinD pod's path MTU (1500 drops large TLS-handshake packets — see NestedNetworkMTU).
	err = registry.EnsureNetwork(
		ctx, dindClient, clusterName, networkCIDR, registry.NestedNetworkMTU, writer,
	)
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

	return p.applyMirrorsToTalosConfig(clusterName)
}

// applyMirrorsToTalosConfig points the nested Talos machine config's
// registries.mirrors at the in-DinD registry containers (by Docker DNS name).
func (p *KubernetesProvisioner) applyMirrorsToTalosConfig(clusterName string) error {
	mirrors := registry.BuildTalosMirrorRegistries(p.mirrorSpecs, clusterName)

	err := p.inner.talosConfigs.ApplyMirrorRegistries(mirrors)
	if err != nil {
		return fmt.Errorf("apply nested mirror registries to talos config: %w", err)
	}

	return nil
}

// nestedDebugEnvVar gates the nested-cluster diagnostic dump. It is off by default
// (the dump streams container logs, which are verbose and could contain sensitive
// data); set it to a non-empty value to opt in (CI enables it for the nested-provider
// system test to capture bootstrap-failure diagnostics).
const nestedDebugEnvVar = "KSAIL_NESTED_DEBUG"

// dumpNestedDiagnostics is a best-effort, opt-in diagnostic that dumps the tail of
// logs for the DinD containers belonging to this cluster (the Talos node containers
// and the pull-through mirror containers, named "<cluster>-..."). It is invoked when
// the nested Talos bootstrap readiness check fails, to reveal why the kubelet stalls
// in "Preparing" (e.g. image-pull errors on the node, or upstream/auth failures on
// the mirrors). It is a no-op unless KSAIL_NESTED_DEBUG is set, so the default path
// only returns the bootstrap error without streaming logs.
func (p *KubernetesProvisioner) dumpNestedDiagnostics(
	ctx context.Context,
	dindClient dockerpkg.Client,
	clusterName string,
) {
	if os.Getenv(nestedDebugEnvVar) == "" {
		return
	}

	writer := p.inner.logWriter

	containers, err := dindClient.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		_, _ = fmt.Fprintf(writer, "diagnostics: failed to list DinD containers: %v\n", err)

		return
	}

	// Match only this cluster's containers by name prefix ("<cluster>-") so we don't
	// accidentally capture (and stream logs from) unrelated containers.
	prefix := clusterName + "-"

	for idx := range containers {
		cnt := containers[idx]

		name := ""
		if len(cnt.Names) > 0 {
			name = strings.TrimPrefix(cnt.Names[0], "/")
		}

		if !strings.HasPrefix(name, prefix) {
			continue
		}

		_, _ = fmt.Fprintf(
			writer,
			"\n===== DinD container %q (image=%s state=%s) logs (tail 100) =====\n",
			name, cnt.Image, cnt.State,
		)

		logs, logErr := dindClient.ContainerLogs(ctx, cnt.ID, container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Tail:       "100",
		})
		if logErr != nil {
			_, _ = fmt.Fprintf(writer, "diagnostics: logs for %q failed: %v\n", name, logErr)

			continue
		}

		_, _ = stdcopy.StdCopy(writer, writer, logs)
		_ = logs.Close()
	}
}

// setupDinD creates the namespace and DinD pod, then waits for readiness.
func (p *KubernetesProvisioner) setupDinD(ctx context.Context, clusterName string) error {
	//nolint:wrapcheck // DinDLifecycle.SetupDinD already wraps with "setup DinD" context
	return p.dindLifecycle().SetupDinD(ctx, clusterName, p.distribution, p.persistence)
}

// dindLifecycle bundles the shared DinD delete/exists/list/setup flow for this
// Talos-on-Kubernetes provisioner. Talos has no host-kubeconfig cleanup on Delete
// (KubeconfigPath is left empty so the shared Delete skips it).
func (p *KubernetesProvisioner) dindLifecycle() nested.DinDLifecycle {
	return nested.DinDLifecycle{
		Provider:      p.k8sProvider,
		DynamicClient: p.dynamicClient,
		RestConfig:    p.restConfig,
	}
}

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
