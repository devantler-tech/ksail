package talosprovisioner

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/svc/detector"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	dockerprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/docker"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kernelmod"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/provision"
	"github.com/siderolabs/talos/pkg/provision/providers"
)

// TalosProviderName is the name used by the Talos SDK for Docker provisioner.
const TalosProviderName = "docker"

// Docker label keys used by Talos provisioner. These are re-exported from the
// Docker provider package, which owns the canonical Talos label scheme, so the
// two never drift.
const (
	// LabelTalosOwned marks a container as owned by Talos provisioner.
	LabelTalosOwned = dockerprovider.LabelTalosOwned
	// LabelTalosClusterName identifies which cluster a container belongs to.
	LabelTalosClusterName = dockerprovider.LabelTalosClusterName
)

// Node role constants.
const (
	// RoleControlPlane is the role identifier for control-plane nodes.
	RoleControlPlane = "control-plane"
	// RoleWorker is the role identifier for worker nodes.
	RoleWorker = "worker"
)

// Default resource values for nodes.
const (
	defaultMTU = 1500
	// ipv4Offset is the offset from gateway for node IPs (gateway is .1, nodes start at .2).
	ipv4Offset = 2
	// stateDirectoryPermissions is the permissions for the state directory.
	stateDirectoryPermissions = 0o750
	// clusterReadinessTimeout is the timeout for waiting for the cluster to become ready.
	// This is shared across all Talos providers (Docker, Hetzner, Omni) and matches
	// the upstream talosctl default of 20 minutes to accommodate slower cloud bring-up.
	clusterReadinessTimeout = 20 * time.Minute
	// talosAPIWaitTimeout is the timeout for waiting for Talos API to be reachable.
	talosAPIWaitTimeout = 5 * time.Minute
	// bootstrapTimeout is the timeout for bootstrap operations.
	bootstrapTimeout = 2 * time.Minute
	// preBootPollInterval is the polling interval for pre-boot sequence checks.
	// Matches the Talos SDK's default of 5 seconds per check.
	preBootPollInterval = 5 * time.Second
	// retryInterval is the default interval between retry attempts.
	retryInterval = 5 * time.Second
	// longRetryInterval is the interval for longer operations.
	longRetryInterval = 10 * time.Second
	// initialIPCapacity is the initial capacity for IP address slices.
	initialIPCapacity = 2
	// grpcFailedPrecondition is the gRPC status code for FailedPrecondition.
	grpcFailedPrecondition = 9
)

// IP byte shift constants for IPv4 address manipulation.
const (
	byte0Shift = 24
	byte1Shift = 16
	byte2Shift = 8
)

// HetznerInfra holds shared Hetzner infrastructure resource IDs.
// These resources are created once and shared across all node groups.
type HetznerInfra struct {
	NetworkID        int64
	FirewallID       int64
	PlacementGroupID int64
	SSHKeyID         int64
}

// HetznerNodeGroupOpts specifies parameters for creating a group of Hetzner nodes.
type HetznerNodeGroupOpts struct {
	ClusterName string
	Role        string // "control-plane" or "worker"
	Count       int
	ServerType  string
	ISOID       int64 // ISO ID (for Talos public ISOs) - mutually exclusive with ImageID
	ImageID     int64 // snapshot image ID - mutually exclusive with ISOID
	Location    string
}

// Image pull retry defaults.
// ghcr.io may experience transient 5xx errors during image pulls.
const (
	defaultImagePullMaxRetries    = 3
	defaultImagePullRetryBaseWait = 5 * time.Second
	defaultImagePullRetryMaxWait  = 30 * time.Second
)

// imagePullRetryConfig holds retry parameters for Docker image pulls.
type imagePullRetryConfig struct {
	maxRetries int
	baseWait   time.Duration
	maxWait    time.Duration
}

// defaultImagePullRetryConfig returns the default retry configuration for Talos image pulls.
func defaultImagePullRetryConfig() imagePullRetryConfig {
	return imagePullRetryConfig{
		maxRetries: defaultImagePullMaxRetries,
		baseWait:   defaultImagePullRetryBaseWait,
		maxWait:    defaultImagePullRetryMaxWait,
	}
}

// kubeconfigFetcher abstracts the Talos client methods needed for kubeconfig refresh.
// Using an interface allows test doubles without requiring real Talos API connectivity.
type kubeconfigFetcher interface {
	Kubeconfig(ctx context.Context) ([]byte, error)
	Close() error
}

// Provisioner implements ClusterProvisioner for Talos-in-Docker clusters.
type Provisioner struct {
	// talosConfigs holds the loaded Talos machine configurations with all patches applied.
	talosConfigs *talosconfigmanager.Configs
	// options holds runtime configuration for provisioning.
	options *Options
	// dockerClient is used for Docker-specific operations (volume cleanup, port inspection).
	dockerClient dockerclient.Client
	// infraProvider is the infrastructure provider for node operations (start/stop).
	// If nil, falls back to dockerClient for backwards compatibility.
	infraProvider provider.Provider
	// talosOpts holds Talos-specific options (node counts, cloud ISO, etc.).
	talosOpts *v1alpha1.OptionsTalos
	// hetznerOpts holds Hetzner-specific options when using the Hetzner provider.
	hetznerOpts *v1alpha1.OptionsHetzner
	// clusterEndpointIP is the effective Kubernetes API endpoint rendered into
	// the machine configs by updateConfigsWithEndpoint — the cluster's floating
	// IP when FloatingIPEnabled, else the first control-plane node's reachable
	// address. saveHetznerKubeconfig rewrites the saved kubeconfig to it so the
	// file survives control-plane replacement when the endpoint is stable.
	clusterEndpointIP string
	// omniOpts holds Omni-specific options when using the Omni provider.
	omniOpts           *v1alpha1.OptionsOmni
	provisionerFactory func(ctx context.Context) (provision.Provisioner, error)
	// kernelModuleLoader ensures required kernel modules are loaded before
	// Docker-based provisioning. Defaults to kernelmod.EnsureBrNetfilter; tests
	// override it via export_test.go to avoid invoking modprobe.
	kernelModuleLoader func(ctx context.Context, logWriter io.Writer) error
	// talosClientFactory creates a Talos client for the given node IP.
	// Tests can override this via export_test.go to inject a mock.
	talosClientFactory func(ctx context.Context, ip string) (kubeconfigFetcher, error)
	// nodeReachabilityCheck reports when a node's Talos API (apid) is accepting
	// connections at ip:talosAPIPort, retrying until it succeeds or the context is
	// done. It gates Docker scale-up: a freshly started container returns from
	// ContainerStart before apid is listening, so the update's in-place config
	// reconciliation must wait for it. Defaults to a TCP dial loop; tests override
	// it via export_test.go to avoid real network I/O.
	nodeReachabilityCheck func(ctx context.Context, ip string) error
	// nodeConfigFetcher returns the running Talos machine config for a node by IP.
	// Defaults to fetchNodeConfig; tests override it via export_test.go to inject a
	// known running config without real Talos API connectivity (used by the per-node
	// desired-config rebuild — see fetchAndBuildDesiredNodeConfig).
	nodeConfigFetcher func(ctx context.Context, nodeIP string) (talosconfig.Provider, error)
	logWriter         io.Writer
	logMu             sync.Mutex
	componentDetector *detector.ComponentDetector
	// imagePullRetry controls retry behavior for Docker image pulls.
	// Tests can override this via WithImagePullRetryConfig to use near-zero delays.
	imagePullRetry imagePullRetryConfig
	// talosAPIRetry controls retry behavior for transient per-node Talos API failures.
	// Tests can override this via WithTalosAPIRetryConfig to use near-zero delays.
	talosAPIRetry talosAPIRetryConfig
	// snapshotManager manages Talos snapshot images on Hetzner Cloud.
	// Set when the Hetzner provider is configured and schematic-based snapshots are used.
	snapshotManager *hetzner.SnapshotManager
	// deleteStorage controls whether Talos snapshot images are deleted alongside the cluster.
	// When true, DeleteTalosSnapshots is called during cluster deletion on Hetzner.
	deleteStorage bool
	// drainForce, when true, makes node drains delete pods directly instead of via
	// the Eviction API, bypassing PodDisruptionBudgets. It is request-scoped: set
	// from UpdateOptions.Force at the start of an update (see applyUpdateChanges)
	// and read by drainNode. The provisioner is created per command invocation and
	// used sequentially, so a field is safe here.
	drainForce bool
}

// NewProvisioner creates a new Provisioner.
// The talosConfigs parameter contains the pre-loaded Talos machine configurations
// with all patches (file-based and runtime) already applied.
// The options parameter contains runtime settings like node counts and output paths.
func NewProvisioner(
	talosConfigs *talosconfigmanager.Configs,
	options *Options,
) *Provisioner {
	if options == nil {
		options = NewOptions()
	}

	prov := &Provisioner{
		talosConfigs: talosConfigs,
		options:      options,
		provisionerFactory: func(ctx context.Context) (provision.Provisioner, error) {
			return providers.Factory(ctx, TalosProviderName)
		},
		kernelModuleLoader: kernelmod.EnsureBrNetfilter,
		logWriter:          os.Stdout,
		imagePullRetry:     defaultImagePullRetryConfig(),
		talosAPIRetry:      defaultTalosAPIRetryConfig(),
	}

	prov.talosClientFactory = func(ctx context.Context, ip string) (kubeconfigFetcher, error) {
		return prov.createTalosClient(ctx, ip)
	}

	prov.nodeReachabilityCheck = func(ctx context.Context, ip string) error {
		return dialTCPUntilReachable(ctx, ip, talosAPIWaitTimeout, retryInterval)
	}

	prov.nodeConfigFetcher = prov.fetchNodeConfig

	return prov
}

// WithDockerClient sets the Docker client for container operations.
func (p *Provisioner) WithDockerClient(c dockerclient.Client) *Provisioner {
	p.dockerClient = c

	return p
}

// WithProvisionerFactory sets a custom provisioner factory for testing.
func (p *Provisioner) WithProvisionerFactory(
	f func(ctx context.Context) (provision.Provisioner, error),
) *Provisioner {
	p.provisionerFactory = f

	return p
}

// WithLogWriter sets the log writer for provisioning output.
func (p *Provisioner) WithLogWriter(w io.Writer) *Provisioner {
	p.logWriter = w

	return p
}

// WithInfraProvider sets the infrastructure provider for node operations.
func (p *Provisioner) WithInfraProvider(prov provider.Provider) *Provisioner {
	p.infraProvider = prov

	return p
}

// WithHetznerOptions sets the Hetzner-specific options for cloud provisioning.
func (p *Provisioner) WithHetznerOptions(opts v1alpha1.OptionsHetzner) *Provisioner {
	p.hetznerOpts = &opts

	return p
}

// WithOmniOptions sets the Omni-specific options for Omni provisioning.
func (p *Provisioner) WithOmniOptions(opts v1alpha1.OptionsOmni) *Provisioner {
	p.omniOpts = &opts

	return p
}

// WithTalosOptions sets the Talos-specific options (node counts, cloud ISO, etc.).
func (p *Provisioner) WithTalosOptions(opts v1alpha1.OptionsTalos) *Provisioner {
	p.talosOpts = &opts

	return p
}

// WithComponentDetector sets the component detector for querying cluster state.
func (p *Provisioner) WithComponentDetector(d *detector.ComponentDetector) *Provisioner {
	p.componentDetector = d

	return p
}

// WithImagePullRetryConfig overrides the image pull retry parameters.
// Useful in tests to use near-zero delays.
func (p *Provisioner) WithImagePullRetryConfig(
	maxRetries int,
	baseWait, maxWait time.Duration,
) *Provisioner {
	p.imagePullRetry = imagePullRetryConfig{
		maxRetries: maxRetries,
		baseWait:   baseWait,
		maxWait:    maxWait,
	}

	return p
}

// WithTalosAPIRetryConfig overrides the retry parameters for transient
// per-node Talos API failures. Useful in tests to use near-zero delays.
func (p *Provisioner) WithTalosAPIRetryConfig(
	maxAttempts int,
	baseWait, maxWait time.Duration,
) *Provisioner {
	p.talosAPIRetry = talosAPIRetryConfig{
		maxAttempts: maxAttempts,
		baseWait:    baseWait,
		maxWait:     maxWait,
	}

	return p
}

// WithSnapshotManager sets the Hetzner snapshot manager used for Talos OS disk image lifecycle.
func (p *Provisioner) WithSnapshotManager(sm *hetzner.SnapshotManager) *Provisioner {
	p.snapshotManager = sm

	return p
}

// WithDeleteStorage controls whether Talos snapshot images are deleted alongside the cluster.
// When true, DeleteTalosSnapshots is called during cluster deletion on Hetzner.
func (p *Provisioner) WithDeleteStorage(deleteStorage bool) *Provisioner {
	p.deleteStorage = deleteStorage

	return p
}

// SetProvider sets the infrastructure provider for node operations.
// This implements the ProviderAware interface.
func (p *Provisioner) SetProvider(prov provider.Provider) {
	p.infraProvider = prov
}

// SetComponentDetector sets the component detector for querying cluster state.
// This implements the ComponentDetectorAware interface.
func (p *Provisioner) SetComponentDetector(d *detector.ComponentDetector) {
	p.WithComponentDetector(d)
}

// RefreshKubeconfig fetches and saves the kubeconfig for a running cluster.
// For Omni clusters, the kubeconfig is first retrieved from the Omni API;
// if that fails, it falls back to the Talos API via a control-plane node.
// For all other providers (Docker, Hetzner), the kubeconfig is fetched
// from the Talos control-plane API. Docker uses mapped host ports; Hetzner
// uses the node's public IP directly.
// This implements the KubeconfigRefresher interface.
func (p *Provisioner) RefreshKubeconfig(ctx context.Context, name string) error {
	clusterName := p.resolveClusterName(name)

	// Omni: use the Omni SaaS API (Talos API fallback is not viable because
	// getOmniNodesByRole returns machine IDs, not reachable IPs).
	if p.omniOpts != nil {
		omniProv, err := p.omniProvider()
		if err != nil {
			return fmt.Errorf("omni provider for kubeconfig refresh: %w", err)
		}

		return p.saveOmniKubeconfig(ctx, omniProv, clusterName)
	}

	return p.refreshKubeconfigFromTalosAPI(ctx, clusterName)
}

// Options returns the current runtime options.
func (p *Provisioner) Options() *Options {
	return p.options
}

// TalosConfigs returns the loaded Talos machine configurations.
func (p *Provisioner) TalosConfigs() *talosconfigmanager.Configs {
	return p.talosConfigs
}

// Create creates a Talos cluster.
// If name is non-empty, it overrides the cluster name from talosConfigs.
// Routes to Docker-based, Hetzner-based, or Omni-based provisioning based on configuration.
func (p *Provisioner) Create(ctx context.Context, name string) error {
	clusterName := p.resolveClusterName(name)

	// Route to Hetzner-based provisioning if Hetzner options are set
	if p.hetznerOpts != nil {
		return p.createHetznerCluster(ctx, clusterName)
	}

	// Route to Omni-based provisioning if Omni options are set
	if p.omniOpts != nil {
		return p.createOmniCluster(ctx, clusterName)
	}

	// Docker-based provisioning (default)
	return p.createDockerCluster(ctx, clusterName)
}

// Delete deletes a Talos cluster.
// If name is non-empty, it overrides the configured cluster name.
// Routes to Docker-based, Hetzner-based, or Omni-based deletion based on configuration.
func (p *Provisioner) Delete(ctx context.Context, name string) error {
	clusterName := p.resolveClusterName(name)

	// Route to Hetzner-based deletion if Hetzner options are set
	if p.hetznerOpts != nil {
		return p.deleteHetznerCluster(ctx, clusterName)
	}

	// Route to Omni-based deletion if Omni options are set
	if p.omniOpts != nil {
		return p.deleteOmniCluster(ctx, clusterName)
	}

	// Docker-based deletion (default)
	return p.deleteDockerCluster(ctx, clusterName)
}

// Exists checks if a Talos cluster exists.
// If name is non-empty, it overrides the configured cluster name.
// Routes to Docker-based, Hetzner-based, or Omni-based existence check based on configuration.
func (p *Provisioner) Exists(ctx context.Context, name string) (bool, error) {
	clusterName := p.resolveClusterName(name)

	// Route to Hetzner-based check if Hetzner options are set
	if p.hetznerOpts != nil {
		hetznerProv, ok := p.infraProvider.(*hetzner.Provider)
		if !ok {
			return false, fmt.Errorf("%w: got %T", ErrHetznerProviderRequired, p.infraProvider)
		}

		exists, err := hetznerProv.NodesExist(ctx, clusterName)
		if err != nil {
			return false, fmt.Errorf("failed to check if cluster exists: %w", err)
		}

		return exists, nil
	}

	// Route to Omni-based check if Omni options are set
	if p.omniOpts != nil {
		omniProv, err := p.omniProvider()
		if err != nil {
			return false, err
		}

		exists, err := omniProv.ClusterExists(ctx, clusterName)
		if err != nil {
			return false, fmt.Errorf("failed to check if cluster exists: %w", err)
		}

		return exists, nil
	}

	// Docker-based check (default): route through the Docker provider so the
	// Talos provisioner does not duplicate container-listing logic.
	dockerProv, err := p.dockerNodeProvider()
	if err != nil {
		return false, err
	}

	exists, err := dockerProv.NodesExist(ctx, clusterName)
	if err != nil {
		return false, fmt.Errorf("failed to check if cluster exists: %w", err)
	}

	return exists, nil
}

// List lists all Talos-in-Docker clusters.
// Returns unique cluster names from containers with Talos labels.
// It routes through the Docker provider's ListAllClusters so the Talos
// provisioner does not duplicate the container-scanning logic.
func (p *Provisioner) List(ctx context.Context) ([]string, error) {
	dockerProv, err := p.dockerNodeProvider()
	if err != nil {
		return nil, err
	}

	clusters, err := dockerProv.ListAllClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list clusters: %w", err)
	}

	return clusters, nil
}

// Start starts a stopped Talos-in-Docker cluster.
// If name is non-empty, it overrides the configured cluster name.
// Node start is delegated to the infrastructure provider; readiness waiting is
// then specialized per provider type.
func (p *Provisioner) Start(ctx context.Context, name string) error {
	clusterName, infraProvider, err := p.beginNodeLifecycleOp(name, "Starting")
	if err != nil {
		return err
	}

	err = infraProvider.StartNodes(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to start cluster %q: %w", clusterName, err)
	}

	// Wait for cluster to be ready based on provider type
	switch infraProvider.(type) {
	case *hetzner.Provider:
		// Hetzner requires special readiness checks with server discovery
		err = p.waitForHetznerClusterReadyAfterStart(ctx, clusterName)
		if err != nil {
			return fmt.Errorf("cluster started but not ready: %w", err)
		}
	case *dockerprovider.Provider:
		// Docker containers start quickly, but Talos API needs time to initialize
		err = p.waitForDockerClusterReadyAfterStart(ctx, clusterName)
		if err != nil {
			return fmt.Errorf("cluster started but not ready: %w", err)
		}
	}

	_, _ = fmt.Fprintf(p.logWriter, "Successfully started Talos cluster %q\n", clusterName)

	return nil
}

// Stop stops a running Talos-in-Docker cluster.
// If name is non-empty, it overrides the configured cluster name.
// Node stop is delegated to the infrastructure provider.
func (p *Provisioner) Stop(ctx context.Context, name string) error {
	clusterName, infraProvider, err := p.beginNodeLifecycleOp(name, "Stopping")
	if err != nil {
		return err
	}

	err = infraProvider.StopNodes(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to stop cluster %q: %w", clusterName, err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "Successfully stopped Talos cluster %q\n", clusterName)

	return nil
}

// beginNodeLifecycleOp resolves the target cluster name, fetches the node-lifecycle provider, and
// logs the given present-participle verb (e.g. "Starting", "Stopping") — the setup shared by Start
// and Stop before they diverge on which provider method to call and how to wait for readiness.
func (p *Provisioner) beginNodeLifecycleOp(name, verb string) (string, provider.Provider, error) {
	clusterName := p.resolveClusterName(name)

	infraProvider, err := p.nodeLifecycleProvider()
	if err != nil {
		return "", nil, err
	}

	_, _ = fmt.Fprintf(p.logWriter, "%s Talos cluster %q...\n", verb, clusterName)

	return clusterName, infraProvider, nil
}

// nodeLifecycleProvider returns the infrastructure provider used for node
// start/stop operations. When no provider was injected (Docker-only
// construction, e.g. tests), it builds a Docker provider on the canonical Talos
// scheme from the Docker client, returning ErrDockerNotAvailable when neither is
// available.
func (p *Provisioner) nodeLifecycleProvider() (provider.Provider, error) {
	if p.infraProvider != nil {
		return p.infraProvider, nil
	}

	return p.dockerNodeProvider()
}

// refreshKubeconfigFromTalosAPI discovers a control-plane node, connects to its
// Talos API, fetches the kubeconfig, and writes it to disk. For Docker clusters
// it resolves mapped host ports; for Hetzner/Omni it uses the node IP directly.
func (p *Provisioner) refreshKubeconfigFromTalosAPI(ctx context.Context, clusterName string) error {
	nodes, err := p.getNodesByRole(ctx, clusterName)
	if err != nil {
		return fmt.Errorf(
			"failed to list nodes for kubeconfig refresh for cluster %q: %w",
			clusterName,
			err,
		)
	}

	var cpIP string

	for _, node := range nodes {
		if node.Role == RoleControlPlane {
			cpIP = node.IP

			break
		}
	}

	if cpIP == "" {
		return fmt.Errorf(
			"%w: no control-plane nodes found for cluster %q",
			ErrNoControlPlaneForRefresh, clusterName,
		)
	}

	talosEndpoint := cpIP
	k8sEndpoint := "https://" + net.JoinHostPort(cpIP, "6443")

	if p.isDockerProvider() {
		talosEndpoint, err = p.getMappedTalosAPIEndpoint(ctx, clusterName)
		if err != nil {
			return fmt.Errorf("failed to resolve mapped Talos API endpoint: %w", err)
		}

		mappedK8sHost, mappedErr := p.getMappedK8sAPIEndpoint(ctx, clusterName)
		if mappedErr != nil {
			return fmt.Errorf("failed to resolve mapped K8s API endpoint: %w", mappedErr)
		}

		k8sEndpoint = "https://" + mappedK8sHost
	}

	return p.fetchAndWriteKubeconfigForCP(ctx, talosEndpoint, k8sEndpoint)
}

// fetchAndWriteKubeconfigForCP fetches the kubeconfig from the Talos API at talosEndpoint,
// rewrites the server endpoint to k8sEndpoint, and writes the result to disk.
// Docker clusters pass mapped host ports for both endpoints; other providers pass the
// raw control-plane IP (with k8sEndpoint as https://cpIP:6443).
func (p *Provisioner) fetchAndWriteKubeconfigForCP(
	ctx context.Context,
	talosEndpoint, k8sEndpoint string,
) error {
	// Fetching the kubeconfig is an idempotent read, so the whole create+fetch is
	// retried on transient apid failures with a fresh client per attempt. It goes
	// through talosClientFactory (rather than createTalosClient directly) so tests
	// can inject a mock fetcher.
	var kubeconfig []byte

	err := p.retryTransientTalosAPICall(ctx, talosEndpoint, "Kubeconfig fetch",
		func(ctx context.Context) error {
			talosClient, createErr := p.talosClientFactory(ctx, talosEndpoint)
			if createErr != nil {
				return createErr
			}

			defer talosClient.Close() //nolint:errcheck

			fetched, fetchErr := talosClient.Kubeconfig(ctx)
			if fetchErr != nil {
				return fmt.Errorf("kubeconfig request: %w", fetchErr)
			}

			kubeconfig = fetched

			return nil
		})
	if err != nil {
		return fmt.Errorf("failed to fetch kubeconfig from Talos API: %w", err)
	}

	kubeconfig, err = rewriteKubeconfigEndpoint(
		kubeconfig,
		k8sEndpoint,
	)
	if err != nil {
		return fmt.Errorf("failed to rewrite kubeconfig endpoint: %w", err)
	}

	return p.writeKubeconfig(kubeconfig)
}

// resolveClusterName returns the provided name if non-empty, otherwise the cluster name from configs.
func (p *Provisioner) resolveClusterName(name string) string {
	if name != "" {
		return name
	}

	if p.talosConfigs != nil {
		return p.talosConfigs.Name
	}

	return talosconfigmanager.DefaultClusterName
}

// logf writes a formatted message to the log writer.
// It is safe for concurrent use by multiple goroutines.
func (p *Provisioner) logf(format string, args ...any) {
	p.logMu.Lock()
	defer p.logMu.Unlock()

	_, _ = fmt.Fprintf(p.logWriter, format, args...)
}

// syncLogWriter returns an io.Writer that serializes all Write calls through p.logMu.
// Use this whenever p.logWriter is passed to a component that will write from multiple goroutines.
func (p *Provisioner) syncLogWriter() io.Writer {
	return &syncWriter{mu: &p.logMu, w: p.logWriter}
}

// syncWriter wraps an io.Writer with a mutex to make Write goroutine-safe.
type syncWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (sw *syncWriter) Write(b []byte) (int, error) {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	return sw.w.Write(b) //nolint:wrapcheck // transparent delegation
}
