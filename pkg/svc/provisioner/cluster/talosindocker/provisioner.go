package talosindockerprovisioner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/siderolabs/talos/pkg/machinery/config/bundle"
	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/provision"
	"github.com/siderolabs/talos/pkg/provision/access"
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
)

// IP byte shift constants for IPv4 address manipulation.
const (
	byte0Shift = 24
	byte1Shift = 16
	byte2Shift = 8
)

// Common errors for the TalosInDocker provisioner.
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
)

// TalosInDockerProvisioner implements ClusterProvisioner for Talos-in-Docker clusters.
type TalosInDockerProvisioner struct {
	config             *TalosInDockerConfig
	dockerClient       client.APIClient
	provisionerFactory func(ctx context.Context) (provision.Provisioner, error)
	logWriter          io.Writer
}

// NewTalosInDockerProvisioner creates a new TalosInDockerProvisioner with the given configuration.
func NewTalosInDockerProvisioner(config *TalosInDockerConfig) *TalosInDockerProvisioner {
	if config == nil {
		config = NewTalosInDockerConfig()
	}

	return &TalosInDockerProvisioner{
		config: config,
		provisionerFactory: func(ctx context.Context) (provision.Provisioner, error) {
			return providers.Factory(ctx, TalosProviderName)
		},
		logWriter: os.Stdout,
	}
}

// WithDockerClient sets the Docker client for container operations.
func (p *TalosInDockerProvisioner) WithDockerClient(c client.APIClient) *TalosInDockerProvisioner {
	p.dockerClient = c

	return p
}

// WithProvisionerFactory sets a custom provisioner factory for testing.
func (p *TalosInDockerProvisioner) WithProvisionerFactory(
	f func(ctx context.Context) (provision.Provisioner, error),
) *TalosInDockerProvisioner {
	p.provisionerFactory = f

	return p
}

// WithLogWriter sets the log writer for provisioning output.
func (p *TalosInDockerProvisioner) WithLogWriter(w io.Writer) *TalosInDockerProvisioner {
	p.logWriter = w

	return p
}

// Config returns the current configuration.
func (p *TalosInDockerProvisioner) Config() *TalosInDockerConfig {
	return p.config
}

// Create creates a Talos-in-Docker cluster.
// If name is non-empty, it overrides the configured cluster name.
func (p *TalosInDockerProvisioner) Create(ctx context.Context, name string) error {
	// Verify Docker is available and running
	err := p.checkDockerAvailable(ctx)
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

	// Load patches from configured directories
	patches, err := LoadPatches(p.config)
	if err != nil {
		return fmt.Errorf("failed to load patches: %w", err)
	}

	// Add mirror registry patches if configured
	mirrorPatches := p.createMirrorPatches()
	patches = append(patches, mirrorPatches...)

	// Create config bundle with patches
	configBundle, err := p.createConfigBundle(clusterName, patches)
	if err != nil {
		return fmt.Errorf("failed to create config bundle: %w", err)
	}

	// Provision cluster and save configurations
	cluster, err := p.provisionCluster(ctx, clusterName, configBundle)
	if err != nil {
		return err
	}

	return p.saveClusterConfigs(ctx, cluster, configBundle)
}

// Delete deletes a Talos-in-Docker cluster.
// If name is non-empty, it overrides the configured cluster name.
func (p *TalosInDockerProvisioner) Delete(ctx context.Context, name string) error {
	// Verify Docker is available and running
	err := p.checkDockerAvailable(ctx)
	if err != nil {
		return err
	}

	clusterName := p.resolveClusterName(name)

	// Check if cluster exists
	exists, err := p.Exists(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to check if cluster exists: %w", err)
	}

	if !exists {
		return fmt.Errorf("%w: %s", ErrClusterNotFound, clusterName)
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

	_, _ = fmt.Fprintf(p.logWriter, "Successfully deleted Talos cluster %q\n", clusterName)

	return nil
}

// Exists checks if a Talos-in-Docker cluster exists.
// If name is non-empty, it overrides the configured cluster name.
func (p *TalosInDockerProvisioner) Exists(ctx context.Context, name string) (bool, error) {
	if p.dockerClient == nil {
		return false, ErrDockerNotAvailable
	}

	clusterName := p.resolveClusterName(name)

	// Find containers for this specific cluster
	containers, err := p.dockerClient.ContainerList(ctx, container.ListOptions{
		All: true, // Include stopped containers
		Filters: filters.NewArgs(
			filters.Arg("label", LabelTalosOwned+"=true"),
			filters.Arg("label", LabelTalosClusterName+"="+clusterName),
		),
	})
	if err != nil {
		return false, fmt.Errorf("failed to list containers: %w", err)
	}

	return len(containers) > 0, nil
}

// List lists all Talos-in-Docker clusters.
// Returns unique cluster names from containers with Talos labels.
func (p *TalosInDockerProvisioner) List(ctx context.Context) ([]string, error) {
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
func (p *TalosInDockerProvisioner) Start(_ context.Context, name string) error {
	clusterName := p.resolveClusterName(name)
	_ = clusterName

	return ErrNotImplemented
}

// Stop stops a running Talos-in-Docker cluster.
// If name is non-empty, it overrides the configured cluster name.
func (p *TalosInDockerProvisioner) Stop(_ context.Context, name string) error {
	clusterName := p.resolveClusterName(name)
	_ = clusterName

	return ErrNotImplemented
}

// bootstrapAndSaveKubeconfig bootstraps the cluster and saves the kubeconfig.
func (p *TalosInDockerProvisioner) bootstrapAndSaveKubeconfig(
	ctx context.Context,
	cluster provision.Cluster,
	configBundle *bundle.Bundle,
) error {
	// Create access adapter for cluster operations
	clusterAccess := access.NewAdapter(
		cluster,
		provision.WithTalosConfig(configBundle.TalosConfig()),
	)

	defer func() { _ = clusterAccess.Close() }()

	// Bootstrap the cluster
	_, _ = fmt.Fprintf(p.logWriter, "Bootstrapping cluster...\n")

	err := clusterAccess.Bootstrap(ctx, p.logWriter)
	if err != nil {
		return fmt.Errorf("bootstrap failed: %w", err)
	}

	// Fetch kubeconfig from cluster
	_, _ = fmt.Fprintf(p.logWriter, "Fetching kubeconfig...\n")

	kubeconfig, err := clusterAccess.Kubeconfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch kubeconfig: %w", err)
	}

	// Write kubeconfig to file
	err = os.WriteFile(p.config.KubeconfigPath, kubeconfig, kubeconfigFileMode)
	if err != nil {
		return fmt.Errorf("failed to write kubeconfig: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "Saved kubeconfig to %s\n", p.config.KubeconfigPath)

	return nil
}

// provisionCluster creates the Talos cluster using the SDK.
//
//nolint:ireturn // provision.Cluster is the SDK's interface
func (p *TalosInDockerProvisioner) provisionCluster(
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
func (p *TalosInDockerProvisioner) saveClusterConfigs(
	ctx context.Context,
	cluster provision.Cluster,
	configBundle *bundle.Bundle,
) error {
	// Save talosconfig if path is configured
	if p.config.TalosconfigPath != "" {
		saveErr := configBundle.TalosConfig().Save(p.config.TalosconfigPath)
		if saveErr != nil {
			return fmt.Errorf("failed to save talosconfig: %w", saveErr)
		}

		_, _ = fmt.Fprintf(p.logWriter, "Saved talosconfig to %s\n", p.config.TalosconfigPath)
	}

	// Bootstrap the cluster and retrieve kubeconfig
	if p.config.KubeconfigPath != "" {
		saveErr := p.bootstrapAndSaveKubeconfig(ctx, cluster, configBundle)
		if saveErr != nil {
			return fmt.Errorf("failed to save kubeconfig: %w", saveErr)
		}
	}

	return nil
}

// createMirrorPatches generates in-memory mirror registry patches from configuration.
// Returns an empty slice if no mirror registries are configured.
func (p *TalosInDockerProvisioner) createMirrorPatches() []TalosPatch {
	if len(p.config.MirrorRegistries) == 0 {
		return nil
	}

	// Parse mirror specifications from config
	mirrorSpecs := registry.ParseMirrorSpecs(p.config.MirrorRegistries)

	if len(mirrorSpecs) == 0 {
		return nil
	}

	// Generate the YAML patch content
	patchContent := GenerateMirrorPatchYAML(mirrorSpecs)

	// Create a TalosPatch with cluster scope (applies to all nodes)
	patch := TalosPatch{
		Path:    "in-memory:mirror-registries",
		Scope:   PatchScopeCluster,
		Content: []byte(patchContent),
	}

	return []TalosPatch{patch}
}

// checkDockerAvailable verifies that Docker is configured and running.
func (p *TalosInDockerProvisioner) checkDockerAvailable(ctx context.Context) error {
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

// createConfigBundle creates a Talos config bundle with patches applied.
// Patches are categorized by scope and applied appropriately:
// cluster patches are applied to all nodes, control-plane patches are applied
// to control-plane nodes only, and worker patches are applied to worker nodes only.
func (p *TalosInDockerProvisioner) createConfigBundle(
	clusterName string,
	patches []TalosPatch,
) (*bundle.Bundle, error) {
	// Categorize patches by scope
	clusterPatches, controlPlanePatches, workerPatches, err := categorizePatchesByScope(patches)
	if err != nil {
		return nil, err
	}

	// Calculate control plane endpoint
	cidr, err := netip.ParsePrefix(p.config.NetworkCIDR)
	if err != nil {
		return nil, fmt.Errorf("invalid network CIDR: %w", err)
	}

	// First control plane node IP for endpoint
	controlPlaneIP, err := nthIPInNetwork(cidr, ipv4Offset)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate control plane IP: %w", err)
	}

	controlPlaneEndpoint := "https://" + net.JoinHostPort(controlPlaneIP.String(), "6443")

	// Build bundle options
	bundleOpts := []bundle.Option{
		bundle.WithInputOptions(&bundle.InputOptions{
			ClusterName: clusterName,
			Endpoint:    controlPlaneEndpoint,
			KubeVersion: p.config.KubernetesVersion,
		}),
	}

	// Add patches by scope
	if len(clusterPatches) > 0 {
		bundleOpts = append(bundleOpts, bundle.WithPatch(clusterPatches))
	}

	if len(controlPlanePatches) > 0 {
		bundleOpts = append(bundleOpts, bundle.WithPatchControlPlane(controlPlanePatches))
	}

	if len(workerPatches) > 0 {
		bundleOpts = append(bundleOpts, bundle.WithPatchWorker(workerPatches))
	}

	// Create the bundle
	configBundle, err := bundle.NewBundle(bundleOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create config bundle: %w", err)
	}

	return configBundle, nil
}

// categorizePatchesByScope separates patches into cluster, control-plane, and worker categories.
func categorizePatchesByScope(
	patches []TalosPatch,
) ([]configpatcher.Patch, []configpatcher.Patch, []configpatcher.Patch, error) {
	var clusterPatches, controlPlanePatches, workerPatches []configpatcher.Patch

	for _, patch := range patches {
		configPatch, loadErr := configpatcher.LoadPatch(patch.Content)
		if loadErr != nil {
			return nil, nil, nil, fmt.Errorf("%w: %s: %w", ErrInvalidPatch, patch.Path, loadErr)
		}

		switch patch.Scope {
		case PatchScopeCluster:
			clusterPatches = append(clusterPatches, configPatch)
		case PatchScopeControlPlane:
			controlPlanePatches = append(controlPlanePatches, configPatch)
		case PatchScopeWorker:
			workerPatches = append(workerPatches, configPatch)
		default:
			// Default to cluster scope for unknown scopes
			clusterPatches = append(clusterPatches, configPatch)
		}
	}

	return clusterPatches, controlPlanePatches, workerPatches, nil
}

// buildClusterRequest creates a provision.ClusterRequest from our config.
func (p *TalosInDockerProvisioner) buildClusterRequest(
	clusterName string,
	configBundle *bundle.Bundle,
) (provision.ClusterRequest, error) {
	// Parse network CIDR
	cidr, err := netip.ParsePrefix(p.config.NetworkCIDR)
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
		Image:          p.config.TalosImage,
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
func (p *TalosInDockerProvisioner) buildNodeRequests(
	clusterName string,
	cidr netip.Prefix,
	configBundle *bundle.Bundle,
) ([]provision.NodeRequest, error) {
	nodes := make([]provision.NodeRequest, 0, p.config.ControlPlaneNodes+p.config.WorkerNodes)

	// Control plane nodes - use ControlPlane config from bundle
	for nodeIndex := range p.config.ControlPlaneNodes {
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
	for nodeIndex := range p.config.WorkerNodes {
		nodeIP, ipErr := nthIPInNetwork(cidr, p.config.ControlPlaneNodes+nodeIndex+ipv4Offset)
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

// resolveClusterName returns the provided name if non-empty, otherwise the configured name.
func (p *TalosInDockerProvisioner) resolveClusterName(name string) string {
	if name != "" {
		return name
	}

	return p.config.ClusterName
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
