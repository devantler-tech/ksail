package talosprovisioner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	iopath "github.com/devantler-tech/ksail/v5/pkg/io"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/siderolabs/talos/pkg/cluster/check"
	"github.com/siderolabs/talos/pkg/machinery/config/bundle"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/provision"
	"github.com/siderolabs/talos/pkg/provision/access"
	"github.com/siderolabs/talos/pkg/provision/providers"
	"k8s.io/client-go/tools/clientcmd"
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

// Default resource values for nodes.
const (
	defaultNodeMemory = 2 * 1024 * 1024 * 1024 // 2GB
	defaultNodeCPUs   = 2 * 1000 * 1000 * 1000 // 2 CPU cores
	defaultMTU        = 1500
	// ipv4Offset is the offset from gateway for node IPs (gateway is .1, nodes start at .2).
	ipv4Offset = 2
	// stateDirectoryPermissions is the permissions for the state directory.
	stateDirectoryPermissions = 0o750
	// kubeconfigFileMode is the file mode for kubeconfig files.
	kubeconfigFileMode = 0o600
	// clusterReadinessTimeout is the timeout for waiting for the cluster to become ready.
	// This matches the upstream talosctl default of 10 minutes.
	clusterReadinessTimeout = 10 * time.Minute
)

// IP byte shift constants for IPv4 address manipulation.
const (
	byte0Shift = 24
	byte1Shift = 16
	byte2Shift = 8
)

// Common errors for the Talos provisioner.
var (
	// ErrClusterNotFound is returned when a cluster is not found.
	ErrClusterNotFound = errors.New("cluster not found")
	// ErrDockerNotAvailable is returned when Docker is not available.
	ErrDockerNotAvailable = errors.New("docker is not available: ensure Docker is running")
	// ErrClusterAlreadyExists is returned when attempting to create a cluster that already exists.
	ErrClusterAlreadyExists = errors.New("cluster already exists")
	// ErrInvalidPatch is returned when a patch file is invalid.
	ErrInvalidPatch = errors.New("invalid patch file")
	// ErrNotImplemented is returned when a method is not yet implemented.
	ErrNotImplemented = errors.New("not implemented")
	// ErrIPv6NotSupported is returned when IPv6 addresses are used but not supported.
	ErrIPv6NotSupported = errors.New("IPv6 not supported")
	// ErrNegativeOffset is returned when a negative offset is provided for IP calculation.
	ErrNegativeOffset = errors.New("negative offset not allowed")
	// ErrNoControlPlane is returned when no control plane container is found.
	ErrNoControlPlane = errors.New("no control plane container found")
	// ErrNoPortMapping is returned when no port mapping is found for a required port.
	ErrNoPortMapping = errors.New("no port mapping found")
	// ErrMissingKubernetesEndpoint is returned when the cluster info is missing the Kubernetes endpoint.
	ErrMissingKubernetesEndpoint = errors.New("cluster info missing KubernetesEndpoint")
	// ErrKernelModuleLoadFailed is returned when loading a required kernel module fails.
	ErrKernelModuleLoadFailed = errors.New("failed to load kernel module")
)

// TalosProvisioner implements ClusterProvisioner for Talos-in-Docker clusters.
type TalosProvisioner struct {
	// talosConfigs holds the loaded Talos machine configurations with all patches applied.
	talosConfigs *talosconfigmanager.Configs
	// options holds runtime configuration for provisioning.
	options            *Options
	dockerClient       client.APIClient
	provisionerFactory func(ctx context.Context) (provision.Provisioner, error)
	logWriter          io.Writer
}

// NewTalosProvisioner creates a new TalosProvisioner.
// The talosConfigs parameter contains the pre-loaded Talos machine configurations
// with all patches (file-based and runtime) already applied.
// The options parameter contains runtime settings like node counts and output paths.
func NewTalosProvisioner(
	talosConfigs *talosconfigmanager.Configs,
	options *Options,
) *TalosProvisioner {
	if options == nil {
		options = NewOptions()
	}

	return &TalosProvisioner{
		talosConfigs: talosConfigs,
		options:      options,
		provisionerFactory: func(ctx context.Context) (provision.Provisioner, error) {
			return providers.Factory(ctx, TalosProviderName)
		},
		logWriter: os.Stdout,
	}
}

// WithDockerClient sets the Docker client for container operations.
func (p *TalosProvisioner) WithDockerClient(c client.APIClient) *TalosProvisioner {
	p.dockerClient = c

	return p
}

// WithProvisionerFactory sets a custom provisioner factory for testing.
func (p *TalosProvisioner) WithProvisionerFactory(
	f func(ctx context.Context) (provision.Provisioner, error),
) *TalosProvisioner {
	p.provisionerFactory = f

	return p
}

// WithLogWriter sets the log writer for provisioning output.
func (p *TalosProvisioner) WithLogWriter(w io.Writer) *TalosProvisioner {
	p.logWriter = w

	return p
}

// Options returns the current runtime options.
func (p *TalosProvisioner) Options() *Options {
	return p.options
}

// TalosConfigs returns the loaded Talos machine configurations.
func (p *TalosProvisioner) TalosConfigs() *talosconfigmanager.Configs {
	return p.talosConfigs
}

// Create creates a Talos-in-Docker cluster.
// If name is non-empty, it overrides the cluster name from talosConfigs.
func (p *TalosProvisioner) Create(ctx context.Context, name string) error {
	// Ensure required kernel modules are loaded (Linux only)
	err := p.ensureKernelModules(ctx)
	if err != nil {
		return err
	}

	// Verify Docker is available and running
	err = p.checkDockerAvailable(ctx)
	if err != nil {
		return err
	}

	clusterName := p.resolveClusterName(name)

	// Check if cluster already exists
	exists, err := p.Exists(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to check if cluster exists: %w", err)
	}

	if exists {
		return fmt.Errorf("%w: %s", ErrClusterAlreadyExists, clusterName)
	}

	// Use the pre-loaded configs (already have all patches applied)
	configBundle := p.talosConfigs.Bundle()

	// Provision cluster and save configurations
	cluster, err := p.provisionCluster(ctx, clusterName, configBundle)
	if err != nil {
		return err
	}

	return p.saveClusterConfigs(ctx, cluster, configBundle)
}

// Delete deletes a Talos-in-Docker cluster.
// If name is non-empty, it overrides the configured cluster name.
func (p *TalosProvisioner) Delete(ctx context.Context, name string) error {
	clusterName, err := p.validateClusterOperation(ctx, name)
	if err != nil {
		return err
	}

	// Get state directory for cluster state
	stateDir, err := getStateDirectory()
	if err != nil {
		return fmt.Errorf("failed to get state directory: %w", err)
	}

	// Create Talos provisioner
	talosProvisioner, err := p.provisionerFactory(ctx)
	if err != nil {
		return fmt.Errorf("failed to create Talos provisioner: %w", err)
	}

	defer func() { _ = talosProvisioner.Close() }()

	// Reflect to get cluster object from existing state
	cluster, err := talosProvisioner.Reflect(ctx, clusterName, stateDir)
	if err != nil {
		return fmt.Errorf("failed to reflect cluster state: %w", err)
	}

	// Destroy the cluster
	_, _ = fmt.Fprintf(p.logWriter, "Deleting Talos cluster %q...\n", clusterName)

	err = talosProvisioner.Destroy(ctx, cluster, provision.WithLogWriter(p.logWriter))
	if err != nil {
		return fmt.Errorf("failed to destroy cluster: %w", err)
	}

	// Clean up kubeconfig - remove only the context for this cluster
	if p.options.KubeconfigPath != "" {
		cleanupErr := p.cleanupKubeconfig(clusterName)
		if cleanupErr != nil {
			// Log warning but don't fail the delete operation
			_, _ = fmt.Fprintf(
				p.logWriter,
				"Warning: failed to clean up kubeconfig: %v\n",
				cleanupErr,
			)
		}
	}

	_, _ = fmt.Fprintf(p.logWriter, "Successfully deleted Talos cluster %q\n", clusterName)

	return nil
}

// Exists checks if a Talos-in-Docker cluster exists.
// If name is non-empty, it overrides the configured cluster name.
func (p *TalosProvisioner) Exists(ctx context.Context, name string) (bool, error) {
	if p.dockerClient == nil {
		return false, ErrDockerNotAvailable
	}

	clusterName := p.resolveClusterName(name)

	containers, err := p.listTalosContainers(ctx, clusterName)
	if err != nil {
		return false, fmt.Errorf("failed to list containers: %w", err)
	}

	return len(containers) > 0, nil
}

// List lists all Talos-in-Docker clusters.
// Returns unique cluster names from containers with Talos labels.
func (p *TalosProvisioner) List(ctx context.Context) ([]string, error) {
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

	// Extract unique cluster names
	clusterSet := make(map[string]struct{})

	for _, c := range containers {
		if name, ok := c.Labels[LabelTalosClusterName]; ok && name != "" {
			clusterSet[name] = struct{}{}
		}
	}

	// Convert set to slice
	clusters := make([]string, 0, len(clusterSet))
	for name := range clusterSet {
		clusters = append(clusters, name)
	}

	return clusters, nil
}

// Start starts a stopped Talos-in-Docker cluster.
// If name is non-empty, it overrides the configured cluster name.
func (p *TalosProvisioner) Start(ctx context.Context, name string) error {
	clusterName, containers, err := p.getClusterContainers(ctx, name)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(p.logWriter, "Starting Talos cluster %q...\\n", clusterName)

	// Start each container
	for _, c := range containers {
		err = p.dockerClient.ContainerStart(ctx, c.ID, container.StartOptions{})
		if err != nil {
			return fmt.Errorf("failed to start container %s: %w", c.Names[0], err)
		}
	}

	_, _ = fmt.Fprintf(p.logWriter, "Successfully started Talos cluster %q\\n", clusterName)

	return nil
}

// containerStopTimeout is the timeout for stopping a container gracefully.
const containerStopTimeout = 30

// Stop stops a running Talos-in-Docker cluster.
// If name is non-empty, it overrides the configured cluster name.
func (p *TalosProvisioner) Stop(ctx context.Context, name string) error {
	clusterName, containers, err := p.getClusterContainers(ctx, name)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(p.logWriter, "Stopping Talos cluster %q...\\n", clusterName)

	// Stop each container with a graceful timeout
	timeout := containerStopTimeout
	for _, c := range containers {
		err = p.dockerClient.ContainerStop(ctx, c.ID, container.StopOptions{Timeout: &timeout})
		if err != nil {
			return fmt.Errorf("failed to stop container %s: %w", c.Names[0], err)
		}
	}

	_, _ = fmt.Fprintf(p.logWriter, "Successfully stopped Talos cluster %q\\n", clusterName)

	return nil
}

// listTalosContainers lists all containers for a specific Talos cluster.
func (p *TalosProvisioner) listTalosContainers(
	ctx context.Context,
	clusterName string,
) ([]container.Summary, error) {
	containers, err := p.dockerClient.ContainerList(ctx, container.ListOptions{
		All: true, // Include stopped containers
		Filters: filters.NewArgs(
			filters.Arg("label", LabelTalosOwned+"=true"),
			filters.Arg("label", LabelTalosClusterName+"="+clusterName),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list Talos containers: %w", err)
	}

	return containers, nil
}

// validateClusterOperation validates that Docker is available and the cluster exists.
// Returns the resolved cluster name or an error.
func (p *TalosProvisioner) validateClusterOperation(
	ctx context.Context,
	name string,
) (string, error) {
	// Verify Docker is available and running
	err := p.checkDockerAvailable(ctx)
	if err != nil {
		return "", err
	}

	clusterName := p.resolveClusterName(name)

	// Check if cluster exists
	exists, err := p.Exists(ctx, clusterName)
	if err != nil {
		return "", fmt.Errorf("failed to check if cluster exists: %w", err)
	}

	if !exists {
		return "", fmt.Errorf("%w: %s", ErrClusterNotFound, clusterName)
	}

	return clusterName, nil
}

// getClusterContainers validates the operation and returns the cluster's containers.
// This combines validation with container listing for Start/Stop operations.
func (p *TalosProvisioner) getClusterContainers(
	ctx context.Context,
	name string,
) (string, []container.Summary, error) {
	clusterName, err := p.validateClusterOperation(ctx, name)
	if err != nil {
		return "", nil, err
	}

	containers, err := p.listTalosContainers(ctx, clusterName)
	if err != nil {
		return "", nil, fmt.Errorf("failed to list containers: %w", err)
	}

	return clusterName, containers, nil
}

// bootstrapAndSaveKubeconfig bootstraps the cluster and saves the kubeconfig.
//
//nolint:cyclop,funlen // Bootstrap sequence is inherently complex but logically coherent
func (p *TalosProvisioner) bootstrapAndSaveKubeconfig(
	ctx context.Context,
	cluster provision.Cluster,
	configBundle *bundle.Bundle,
) error {
	// Get the mapped Talos API endpoint for Docker-in-VM environments (macOS, Windows).
	// On these platforms, the container's internal IP is not accessible from the host,
	// so we need to use 127.0.0.1 with the mapped port.
	mappedEndpoint, err := p.getMappedTalosAPIEndpoint(ctx, cluster.Info().ClusterName)
	if err != nil {
		return fmt.Errorf("failed to get mapped Talos API endpoint: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "Using Talos API endpoint: %s\n", mappedEndpoint)

	// Create a modified talosconfig with the mapped endpoint
	talosConfig := configBundle.TalosConfig()
	if talosConfig != nil && talosConfig.Context != "" {
		if context, ok := talosConfig.Contexts[talosConfig.Context]; ok {
			context.Endpoints = []string{mappedEndpoint}
		}
	}

	// Get the Kubernetes API endpoint from the cluster info.
	// The Docker provisioner automatically sets this to the external endpoint
	// (https://127.0.0.1:<mapped-port>) when the cluster is created.
	kubernetesEndpoint := cluster.Info().KubernetesEndpoint
	if kubernetesEndpoint == "" {
		return ErrMissingKubernetesEndpoint
	}

	_, _ = fmt.Fprintf(p.logWriter, "Using Kubernetes API endpoint: %s\n", kubernetesEndpoint)

	// Create access adapter for cluster operations.
	// WithKubernetesEndpoint sets ForceEndpoint which is used to rewrite the kubeconfig.
	clusterAccess := access.NewAdapter(
		cluster,
		provision.WithTalosConfig(talosConfig),
		provision.WithKubernetesEndpoint(kubernetesEndpoint),
	)

	defer func() { _ = clusterAccess.Close() }()

	// Bootstrap the cluster
	_, _ = fmt.Fprintf(p.logWriter, "Bootstrapping cluster...\n")

	err = clusterAccess.Bootstrap(ctx, p.logWriter)
	if err != nil {
		return fmt.Errorf("bootstrap failed: %w", err)
	}

	// Wait for cluster to be ready (Talos API, etcd, Kubernetes API via external endpoint).
	// Since clusterAccess has ForceEndpoint set to the mapped localhost port,
	// K8s checks in DefaultClusterChecks() validate host connectivity.
	_, _ = fmt.Fprintf(p.logWriter, "Waiting for cluster to be ready...\n")

	checkCtx, checkCancel := context.WithTimeout(ctx, clusterReadinessTimeout)
	defer checkCancel()

	// Select appropriate cluster checks based on CNI configuration.
	// When using a custom CNI (e.g., Cilium), skip K8s component checks (CoreDNS, kube-proxy)
	// because pods cannot start until the CNI is installed.
	// See: https://pkg.go.dev/github.com/siderolabs/talos/pkg/cluster/check#K8sComponentsReadinessChecks
	clusterChecks := check.DefaultClusterChecks()

	// Skip CNI-dependent checks if:
	// 1. The Talos config has CNI disabled (scaffolded project with disable-default-cni.yaml patch), OR
	// 2. KSail will install a custom CNI after cluster creation (options.SkipCNIChecks)
	if (p.talosConfigs != nil && p.talosConfigs.IsCNIDisabled()) || p.options.SkipCNIChecks {
		clusterChecks = check.PreBootSequenceChecks()
	}

	err = check.Wait(checkCtx, clusterAccess, clusterChecks, check.StderrReporter())
	if err != nil {
		return fmt.Errorf("cluster readiness check failed: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "Cluster is ready\n")

	// Fetch kubeconfig from cluster
	_, _ = fmt.Fprintf(p.logWriter, "Fetching kubeconfig...\n")

	kubeconfig, err := clusterAccess.Kubeconfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch kubeconfig: %w", err)
	}

	// Rewrite kubeconfig server endpoint using ForceEndpoint (set via WithKubernetesEndpoint).
	// The kubeconfig from Talos uses internal IPs, but we need the mapped localhost endpoint.
	kubeconfig, err = rewriteKubeconfigEndpoint(kubeconfig, clusterAccess.ForceEndpoint)
	if err != nil {
		return fmt.Errorf("failed to rewrite kubeconfig endpoint: %w", err)
	}

	// Expand tilde in kubeconfig path (e.g., ~/.kube/config -> /home/user/.kube/config)
	kubeconfigPath, err := iopath.ExpandHomePath(p.options.KubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to expand kubeconfig path: %w", err)
	}

	// Ensure kubeconfig directory exists
	kubeconfigDir := filepath.Dir(kubeconfigPath)
	if kubeconfigDir != "" && kubeconfigDir != "." {
		mkdirErr := os.MkdirAll(kubeconfigDir, stateDirectoryPermissions)
		if mkdirErr != nil {
			return fmt.Errorf("failed to create kubeconfig directory: %w", mkdirErr)
		}
	}

	// Write kubeconfig to file
	err = os.WriteFile(kubeconfigPath, kubeconfig, kubeconfigFileMode)
	if err != nil {
		return fmt.Errorf("failed to write kubeconfig: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "Saved kubeconfig to %s\n", kubeconfigPath)

	return nil
}

// provisionCluster creates the Talos cluster using the SDK.
//
//nolint:ireturn // provision.Cluster is the SDK's interface
func (p *TalosProvisioner) provisionCluster(
	ctx context.Context,
	clusterName string,
	configBundle *bundle.Bundle,
) (provision.Cluster, error) {
	// Create Talos provisioner
	talosProvisioner, err := p.provisionerFactory(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create Talos provisioner: %w", err)
	}

	defer func() { _ = talosProvisioner.Close() }()

	// Build cluster request with node configs from bundle
	clusterRequest, err := p.buildClusterRequest(clusterName, configBundle)
	if err != nil {
		return nil, fmt.Errorf("failed to build cluster request: %w", err)
	}

	// Create the cluster using Talos provisioner
	cluster, err := talosProvisioner.Create(
		ctx,
		clusterRequest,
		provision.WithLogWriter(p.logWriter),
		provision.WithTalosConfig(configBundle.TalosConfig()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create cluster: %w", err)
	}

	// Log cluster info
	_, _ = fmt.Fprintf(p.logWriter, "Created Talos cluster %q with %d nodes\n",
		cluster.Info().ClusterName,
		len(cluster.Info().Nodes))

	return cluster, nil
}

// saveClusterConfigs saves talosconfig and kubeconfig if paths are configured.
func (p *TalosProvisioner) saveClusterConfigs(
	ctx context.Context,
	cluster provision.Cluster,
	configBundle *bundle.Bundle,
) error {
	// Save talosconfig if path is configured
	if p.options.TalosconfigPath != "" {
		saveErr := configBundle.TalosConfig().Save(p.options.TalosconfigPath)
		if saveErr != nil {
			return fmt.Errorf("failed to save talosconfig: %w", saveErr)
		}

		_, _ = fmt.Fprintf(p.logWriter, "Saved talosconfig to %s\n", p.options.TalosconfigPath)
	}

	// Bootstrap the cluster and retrieve kubeconfig
	if p.options.KubeconfigPath != "" {
		saveErr := p.bootstrapAndSaveKubeconfig(ctx, cluster, configBundle)
		if saveErr != nil {
			return fmt.Errorf("failed to save kubeconfig: %w", saveErr)
		}
	}

	return nil
}

// checkDockerAvailable verifies that Docker is configured and running.
func (p *TalosProvisioner) checkDockerAvailable(ctx context.Context) error {
	if p.dockerClient == nil {
		return ErrDockerNotAvailable
	}

	// Ping Docker to verify it's actually running
	_, err := p.dockerClient.Ping(ctx)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDockerNotAvailable, err)
	}

	return nil
}

// buildClusterRequest creates a provision.ClusterRequest from our config.
func (p *TalosProvisioner) buildClusterRequest(
	clusterName string,
	configBundle *bundle.Bundle,
) (provision.ClusterRequest, error) {
	// Parse the network CIDR
	cidr, err := netip.ParsePrefix(p.options.NetworkCIDR)
	if err != nil {
		return provision.ClusterRequest{}, fmt.Errorf("invalid network CIDR: %w", err)
	}

	// Calculate gateway (first usable IP)
	gatewayIP, err := nthIPInNetwork(cidr, 1)
	if err != nil {
		return provision.ClusterRequest{}, fmt.Errorf("failed to calculate gateway IP: %w", err)
	}

	// State directory for cluster
	stateDir, err := getStateDirectory()
	if err != nil {
		return provision.ClusterRequest{}, fmt.Errorf("failed to get state directory: %w", err)
	}

	// Build node requests with configs from bundle
	nodes, err := p.buildNodeRequests(clusterName, cidr, configBundle)
	if err != nil {
		return provision.ClusterRequest{}, err
	}

	return provision.ClusterRequest{
		Name:           clusterName,
		Image:          p.options.TalosImage,
		StateDirectory: stateDir,
		Network: provision.NetworkRequest{
			Name:         clusterName,
			CIDRs:        []netip.Prefix{cidr},
			GatewayAddrs: []netip.Addr{gatewayIP},
			MTU:          defaultMTU,
		},
		Nodes: nodes,
	}, nil
}

// buildNodeRequests creates node request configurations for control plane and worker nodes.
func (p *TalosProvisioner) buildNodeRequests(
	clusterName string,
	cidr netip.Prefix,
	configBundle *bundle.Bundle,
) ([]provision.NodeRequest, error) {
	nodes := make([]provision.NodeRequest, 0, p.options.ControlPlaneNodes+p.options.WorkerNodes)

	// Control plane nodes - use ControlPlane config from bundle
	for nodeIndex := range p.options.ControlPlaneNodes {
		nodeIP, ipErr := nthIPInNetwork(cidr, nodeIndex+ipv4Offset)
		if ipErr != nil {
			return nil, fmt.Errorf(
				"failed to calculate IP for control-plane-%d: %w",
				nodeIndex+1,
				ipErr,
			)
		}

		nodes = append(nodes, provision.NodeRequest{
			Name:     fmt.Sprintf("%s-control-plane-%d", clusterName, nodeIndex+1),
			Type:     machine.TypeControlPlane,
			IPs:      []netip.Addr{nodeIP},
			Memory:   defaultNodeMemory,
			NanoCPUs: defaultNodeCPUs,
			Config:   configBundle.ControlPlane(),
		})
	}

	// Worker nodes - use Worker config from bundle
	for nodeIndex := range p.options.WorkerNodes {
		nodeIP, ipErr := nthIPInNetwork(cidr, p.options.ControlPlaneNodes+nodeIndex+ipv4Offset)
		if ipErr != nil {
			return nil, fmt.Errorf(
				"failed to calculate IP for worker-%d: %w",
				nodeIndex+1,
				ipErr,
			)
		}

		nodes = append(nodes, provision.NodeRequest{
			Name:     fmt.Sprintf("%s-worker-%d", clusterName, nodeIndex+1),
			Type:     machine.TypeWorker,
			IPs:      []netip.Addr{nodeIP},
			Memory:   defaultNodeMemory,
			NanoCPUs: defaultNodeCPUs,
			Config:   configBundle.Worker(),
		})
	}

	return nodes, nil
}

// resolveClusterName returns the provided name if non-empty, otherwise the cluster name from configs.
func (p *TalosProvisioner) resolveClusterName(name string) string {
	if name != "" {
		return name
	}

	if p.talosConfigs != nil {
		return p.talosConfigs.Name
	}

	return talosconfigmanager.DefaultClusterName
}

// nthIPInNetwork returns the nth IP in the network (1-indexed).
// The offset parameter specifies how many addresses to skip from the network base address.
func nthIPInNetwork(prefix netip.Prefix, offset int) (netip.Addr, error) {
	addr := prefix.Addr()

	// Convert to byte slice for manipulation
	if addr.Is4() {
		ipBytes := addr.As4()
		ipValue := uint32(ipBytes[0])<<byte0Shift |
			uint32(ipBytes[1])<<byte1Shift |
			uint32(ipBytes[2])<<byte2Shift |
			uint32(ipBytes[3])

		// Safe conversion: offset should always be small and positive for valid cluster sizes
		if offset < 0 {
			return netip.Addr{}, ErrNegativeOffset
		}

		//nolint:gosec // G115: offset validated above and bounded by cluster size
		ipValue += uint32(offset)

		return netip.AddrFrom4([4]byte{
			byte(ipValue >> byte0Shift),
			byte(ipValue >> byte1Shift),
			byte(ipValue >> byte2Shift),
			byte(ipValue),
		}), nil
	}

	return netip.Addr{}, ErrIPv6NotSupported
}

// getStateDirectory returns the state directory for Talos clusters.
func getStateDirectory() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	stateDir := filepath.Join(homeDir, ".talos", "clusters")

	mkdirErr := os.MkdirAll(stateDir, stateDirectoryPermissions)
	if mkdirErr != nil {
		return "", fmt.Errorf("failed to create state directory: %w", mkdirErr)
	}

	return stateDir, nil
}

// talosAPIPort is the Talos apid service port.
const talosAPIPort = 50000

// getMappedTalosAPIEndpoint finds the control plane container and returns the mapped Talos API endpoint.
// On macOS and other non-Linux systems, Docker runs in a VM, so we need to use the mapped port
// via 127.0.0.1 instead of the container's internal IP.
func (p *TalosProvisioner) getMappedTalosAPIEndpoint(
	ctx context.Context,
	clusterName string,
) (string, error) {
	if p.dockerClient == nil {
		return "", ErrDockerNotAvailable
	}

	// Find the control plane container for this cluster
	containers, err := p.dockerClient.ContainerList(ctx, container.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", LabelTalosOwned+"=true"),
			filters.Arg("label", LabelTalosClusterName+"="+clusterName),
			filters.Arg("label", "talos.type=controlplane"),
		),
	})
	if err != nil {
		return "", fmt.Errorf("failed to list containers: %w", err)
	}

	if len(containers) == 0 {
		return "", fmt.Errorf("%w for cluster %s", ErrNoControlPlane, clusterName)
	}

	// Get the first control plane container (they all have the same port mapping)
	containerID := containers[0].ID

	// Inspect the container to get port mappings
	inspect, err := p.dockerClient.ContainerInspect(ctx, containerID)
	if err != nil {
		return "", fmt.Errorf("failed to inspect container: %w", err)
	}

	// Find the mapped port for Talos API (50000/tcp)
	portKey := nat.Port(fmt.Sprintf("%d/tcp", talosAPIPort))

	bindings, ok := inspect.NetworkSettings.Ports[portKey]
	if !ok || len(bindings) == 0 {
		return "", fmt.Errorf("%w for Talos API port %d", ErrNoPortMapping, talosAPIPort)
	}

	// Use the first binding's host port
	hostPort := bindings[0].HostPort

	return net.JoinHostPort("127.0.0.1", hostPort), nil
}

// rewriteKubeconfigEndpoint rewrites all cluster server endpoints in the kubeconfig
// to use the specified endpoint. This is used for Docker-in-VM environments where
// the internal container IPs are not accessible from the host.
// This follows the same pattern as the Talos SDK's mergeKubeconfig function.
func rewriteKubeconfigEndpoint(kubeconfigBytes []byte, endpoint string) ([]byte, error) {
	if endpoint == "" {
		return kubeconfigBytes, nil
	}

	kubeConfig, err := clientcmd.Load(kubeconfigBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	// Rewrite server endpoint for all clusters
	for name := range kubeConfig.Clusters {
		kubeConfig.Clusters[name].Server = endpoint
	}

	// Serialize back to YAML
	result, err := clientcmd.Write(*kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize kubeconfig: %w", err)
	}

	return result, nil
}

// cleanupKubeconfig removes the cluster, context, and user entries for the deleted cluster
// from the kubeconfig file. This only removes entries matching the cluster name,
// leaving other cluster configurations intact.
func (p *TalosProvisioner) cleanupKubeconfig(clusterName string) error {
	// Expand tilde in kubeconfig path
	kubeconfigPath, err := iopath.ExpandHomePath(p.options.KubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to expand kubeconfig path: %w", err)
	}

	// Talos uses "admin@<cluster-name>" format for context and user names
	contextName := "admin@" + clusterName
	userName := contextName

	err = k8s.CleanupKubeconfig(
		kubeconfigPath,
		clusterName,
		contextName,
		userName,
		p.logWriter,
	)
	if err != nil {
		return fmt.Errorf("failed to cleanup kubeconfig: %w", err)
	}

	return nil
}

// ensureKernelModules loads required kernel modules for Talos networking.
// On Linux, this loads the br_netfilter module which is required for bridge networking.
// On macOS and Windows, Docker Desktop handles this automatically via its Linux VM.
func (p *TalosProvisioner) ensureKernelModules(ctx context.Context) error {
	// Only needed on Linux - Docker Desktop on macOS/Windows handles this in its VM
	if runtime.GOOS != "linux" {
		return nil
	}

	// Check if br_netfilter is already loaded by reading /proc/modules
	data, err := os.ReadFile("/proc/modules")
	if err == nil {
		// Check if br_netfilter is in the loaded modules list
		if containsModule(string(data), "br_netfilter") {
			return nil // Already loaded
		}
	}

	// Try to load the module using modprobe
	_, _ = fmt.Fprintf(p.logWriter, "Loading br_netfilter kernel module...\n")

	cmd := exec.CommandContext(ctx, "modprobe", "br_netfilter")

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Try with sudo if direct modprobe fails (user may not have CAP_SYS_MODULE)
		sudoCmd := exec.CommandContext(ctx, "sudo", "modprobe", "br_netfilter")

		sudoOutput, sudoErr := sudoCmd.CombinedOutput()
		if sudoErr != nil {
			return fmt.Errorf(
				"%w: br_netfilter (modprobe failed: %w, sudo modprobe failed: %w, output: %s)",
				ErrKernelModuleLoadFailed,
				err,
				sudoErr,
				string(append(output, sudoOutput...)),
			)
		}
	}

	_, _ = fmt.Fprintf(p.logWriter, "Successfully loaded br_netfilter kernel module\n")

	return nil
}

// containsModule checks if a module name appears in /proc/modules output.
func containsModule(modulesContent, moduleName string) bool {
	// /proc/modules format: "module_name size refcount deps state offset"
	// Each module is on its own line, and the name is the first field
	lines := splitLines(modulesContent)
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		// Get the first field (module name)
		fields := splitFields(line)
		if len(fields) > 0 && fields[0] == moduleName {
			return true
		}
	}

	return false
}

// splitLines splits a string into lines.
func splitLines(content string) []string {
	var lines []string

	start := 0

	for i := range len(content) {
		if content[i] == '\n' {
			lines = append(lines, content[start:i])
			start = i + 1
		}
	}

	if start < len(content) {
		lines = append(lines, content[start:])
	}

	return lines
}

// splitFields splits a string by whitespace.
func splitFields(content string) []string {
	var fields []string

	start := -1

	for i := range len(content) {
		isSpace := content[i] == ' ' || content[i] == '\t'
		if !isSpace && start == -1 {
			start = i
		} else if isSpace && start != -1 {
			fields = append(fields, content[start:i])
			start = -1
		}
	}

	if start != -1 {
		fields = append(fields, content[start:])
	}

	return fields
}
