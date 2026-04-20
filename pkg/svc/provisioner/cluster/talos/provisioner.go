package talosprovisioner

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/devantler-tech/ksail/v6/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v6/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v6/pkg/k8s"
	"github.com/devantler-tech/ksail/v6/pkg/svc/detector"
	"github.com/devantler-tech/ksail/v6/pkg/svc/provider"
	dockerprovider "github.com/devantler-tech/ksail/v6/pkg/svc/provider/docker"
	"github.com/devantler-tech/ksail/v6/pkg/svc/provider/hetzner"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	dockerclient "github.com/docker/docker/client"
	"github.com/siderolabs/talos/pkg/provision"
	"github.com/siderolabs/talos/pkg/provision/providers"
)

// TalosProviderName is the name used by the Talos SDK for Docker provisioner.
const TalosProviderName = "docker"

// Docker label keys used by Talos provisioner.
const (
	// LabelTalosOwned marks a container as owned by Talos provisioner.
	LabelTalosOwned = "talos.owned"
	// LabelTalosClusterName identifies which cluster a container belongs to.
	LabelTalosClusterName = "talos.cluster.name"
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
	// kubeconfigFileMode is the file mode for kubeconfig files.
	kubeconfigFileMode = 0o600
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
	ISOID       int64
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

// Provisioner implements ClusterProvisioner for Talos-in-Docker clusters.
type Provisioner struct {
	// talosConfigs holds the loaded Talos machine configurations with all patches applied.
	talosConfigs *talosconfigmanager.Configs
	// options holds runtime configuration for provisioning.
	options *Options
	// dockerClient is used for Docker-specific operations (volume cleanup, port inspection).
	dockerClient dockerclient.APIClient
	// infraProvider is the infrastructure provider for node operations (start/stop).
	// If nil, falls back to dockerClient for backwards compatibility.
	infraProvider provider.Provider
	// talosOpts holds Talos-specific options (node counts, cloud ISO, etc.).
	talosOpts *v1alpha1.OptionsTalos
	// hetznerOpts holds Hetzner-specific options when using the Hetzner provider.
	hetznerOpts *v1alpha1.OptionsHetzner
	// omniOpts holds Omni-specific options when using the Omni provider.
	omniOpts           *v1alpha1.OptionsOmni
	provisionerFactory func(ctx context.Context) (provision.Provisioner, error)
	logWriter          io.Writer
	logMu              sync.Mutex
	componentDetector  *detector.ComponentDetector
	// imagePullRetry controls retry behavior for Docker image pulls.
	// Tests can override this via WithImagePullRetryConfig to use near-zero delays.
	imagePullRetry imagePullRetryConfig
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

	return &Provisioner{
		talosConfigs: talosConfigs,
		options:      options,
		provisionerFactory: func(ctx context.Context) (provision.Provisioner, error) {
			return providers.Factory(ctx, TalosProviderName)
		},
		logWriter:      os.Stdout,
		imagePullRetry: defaultImagePullRetryConfig(),
	}
}

// WithDockerClient sets the Docker client for container operations.
func (p *Provisioner) WithDockerClient(c dockerclient.APIClient) *Provisioner {
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
// For Omni clusters, the kubeconfig is retrieved from the Omni API.
// For Docker and Hetzner clusters, kubeconfig is expected to persist from creation.
// This implements the KubeconfigRefresher interface.
func (p *Provisioner) RefreshKubeconfig(ctx context.Context, name string) error {
	if p.omniOpts == nil {
		return nil
	}

	clusterName := p.resolveClusterName(name)

	omniProv, err := p.omniProvider()
	if err != nil {
		return err
	}

	return p.saveOmniKubeconfig(ctx, omniProv, clusterName)
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

	// Docker-based check (default)
	if p.dockerClient == nil {
		return false, ErrDockerNotAvailable
	}

	containers, err := p.listTalosContainers(ctx, clusterName)
	if err != nil {
		return false, fmt.Errorf("failed to list containers: %w", err)
	}

	return len(containers) > 0, nil
}

// List lists all Talos-in-Docker clusters.
// Returns unique cluster names from containers with Talos labels.
func (p *Provisioner) List(ctx context.Context) ([]string, error) {
	if p.dockerClient == nil {
		return nil, ErrDockerNotAvailable
	}

	// Find all containers owned by Talos provisioner
	containers, err := p.dockerClient.ContainerList(ctx, container.ListOptions{
		All: true, // Include stopped containers
		Filters: filters.NewArgs(
			filters.Arg("label", LabelTalosOwned+"=true"),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	return k8s.UniqueLabelValues(
		containers,
		LabelTalosClusterName,
		func(c container.Summary) map[string]string {
			return c.Labels
		},
	), nil
}

// Start starts a stopped Talos-in-Docker cluster.
// If name is non-empty, it overrides the configured cluster name.
// Uses the infrastructure provider if set, otherwise falls back to Docker client.
func (p *Provisioner) Start(ctx context.Context, name string) error {
	clusterName := p.resolveClusterName(name)

	// Use infrastructure provider if available
	if p.infraProvider != nil {
		_, _ = fmt.Fprintf(p.logWriter, "Starting Talos cluster %q...\n", clusterName)

		err := p.infraProvider.StartNodes(ctx, clusterName)
		if err != nil {
			return fmt.Errorf("failed to start cluster %q: %w", clusterName, err)
		}

		// Wait for cluster to be ready based on provider type
		switch p.infraProvider.(type) {
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

	// Fall back to Docker client for backwards compatibility
	clusterName, containers, err := p.getClusterContainers(ctx, name)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(p.logWriter, "Starting Talos cluster %q...\n", clusterName)

	// Start each container
	for _, c := range containers {
		err = p.dockerClient.ContainerStart(ctx, c.ID, container.StartOptions{})
		if err != nil {
			return fmt.Errorf("failed to start container %s: %w", c.Names[0], err)
		}
	}

	_, _ = fmt.Fprintf(p.logWriter, "Successfully started Talos cluster %q\n", clusterName)

	return nil
}

// containerStopTimeout is the timeout for stopping a container gracefully.
const containerStopTimeout = 30

// Stop stops a running Talos-in-Docker cluster.
// If name is non-empty, it overrides the configured cluster name.
// Uses the infrastructure provider if set, otherwise falls back to Docker client.
func (p *Provisioner) Stop(ctx context.Context, name string) error {
	clusterName := p.resolveClusterName(name)

	// Use infrastructure provider if available
	if p.infraProvider != nil {
		_, _ = fmt.Fprintf(p.logWriter, "Stopping Talos cluster %q...\n", clusterName)

		err := p.infraProvider.StopNodes(ctx, clusterName)
		if err != nil {
			return fmt.Errorf("failed to stop cluster %q: %w", clusterName, err)
		}

		_, _ = fmt.Fprintf(p.logWriter, "Successfully stopped Talos cluster %q\n", clusterName)

		return nil
	}

	// Fall back to Docker client for backwards compatibility
	clusterName, containers, err := p.getClusterContainers(ctx, name)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(p.logWriter, "Stopping Talos cluster %q...\n", clusterName)

	// Stop each container with a graceful timeout
	timeout := containerStopTimeout
	for _, c := range containers {
		err = p.dockerClient.ContainerStop(ctx, c.ID, container.StopOptions{Timeout: &timeout})
		if err != nil {
			return fmt.Errorf("failed to stop container %s: %w", c.Names[0], err)
		}
	}

	_, _ = fmt.Fprintf(p.logWriter, "Successfully stopped Talos cluster %q\n", clusterName)

	return nil
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
