package talosprovisioner

import (
	"context"
	"fmt"
	"net"
	"net/netip"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/kernelmod"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/go-connections/nat"
	"github.com/siderolabs/talos/pkg/cluster/check"
	"github.com/siderolabs/talos/pkg/machinery/config/bundle"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/provision"
	"github.com/siderolabs/talos/pkg/provision/access"
)

// createDockerCluster creates a Talos-in-Docker cluster using the Talos SDK.
func (p *Provisioner) createDockerCluster(ctx context.Context, clusterName string) error {
	// Ensure required kernel modules are loaded (Linux only)
	err := kernelmod.EnsureBrNetfilter(ctx, p.logWriter)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrKernelModuleLoadFailed, err)
	}

	// Verify Docker is available and running
	err = p.checkDockerAvailable(ctx)
	if err != nil {
		return err
	}

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

// deleteDockerCluster deletes a Talos-in-Docker cluster using the Talos SDK.
func (p *Provisioner) deleteDockerCluster(ctx context.Context, clusterName string) error {
	_, err := p.validateClusterOperation(ctx, clusterName)
	if err != nil {
		return err
	}

	// Collect volumes before destroying cluster (they won't be accessible after)
	volumes, err := p.collectClusterVolumes(ctx, clusterName)
	if err != nil {
		return err
	}

	// Destroy cluster using Talos SDK
	err = p.destroyClusterWithProvisioner(ctx, clusterName)
	if err != nil {
		return err
	}

	// Clean up volumes and config files
	p.cleanupVolumesAfterDestroy(ctx, volumes)
	p.cleanupConfigFiles(clusterName)

	_, _ = fmt.Fprintf(p.logWriter, "Successfully deleted Talos cluster %q\n", clusterName)

	return nil
}

// collectClusterVolumes gathers all volumes used by Talos containers before cluster deletion.
// These are anonymous volumes that the Talos SDK doesn't clean up automatically.
func (p *Provisioner) collectClusterVolumes(ctx context.Context, clusterName string) ([]string, error) {
	containers, err := p.listTalosContainers(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to list Talos containers: %w", err)
	}

	return p.collectContainerVolumes(ctx, containers), nil
}

// destroyClusterWithProvisioner reflects cluster state and destroys it using the Talos SDK.
func (p *Provisioner) destroyClusterWithProvisioner(ctx context.Context, clusterName string) error {
	stateDir, err := getStateDirectory()
	if err != nil {
		return fmt.Errorf("failed to get state directory: %w", err)
	}

	talosProvisioner, err := p.provisionerFactory(ctx)
	if err != nil {
		return fmt.Errorf("failed to create Talos provisioner: %w", err)
	}

	defer func() { _ = talosProvisioner.Close() }()

	cluster, err := talosProvisioner.Reflect(ctx, clusterName, stateDir)
	if err != nil {
		return fmt.Errorf("failed to reflect cluster state: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "Deleting Talos cluster %q...\n", clusterName)

	err = talosProvisioner.Destroy(ctx, cluster, provision.WithLogWriter(p.logWriter))
	if err != nil {
		return fmt.Errorf("failed to destroy cluster: %w", err)
	}

	return nil
}

// cleanupVolumesAfterDestroy removes anonymous Docker volumes used by Talos containers.
func (p *Provisioner) cleanupVolumesAfterDestroy(ctx context.Context, volumes []string) {
	if len(volumes) > 0 {
		_, _ = fmt.Fprintf(p.logWriter, "Cleaning up %d Talos node volumes...\n", len(volumes))
		p.removeVolumes(ctx, volumes)
	}
}

// cleanupConfigFiles removes kubeconfig and talosconfig contexts for the deleted cluster.
// Logs warnings but doesn't fail the delete operation if cleanup fails.
func (p *Provisioner) cleanupConfigFiles(clusterName string) {
	if p.options.KubeconfigPath != "" {
		if err := p.cleanupKubeconfig(clusterName); err != nil {
			_, _ = fmt.Fprintf(
				p.logWriter,
				"Warning: failed to clean up kubeconfig: %v\n",
				err,
			)
		}
	}

	if p.options.TalosconfigPath != "" {
		if err := p.cleanupTalosconfig(clusterName); err != nil {
			_, _ = fmt.Fprintf(
				p.logWriter,
				"Warning: failed to clean up talosconfig: %v\n",
				err,
			)
		}
	}
}

// listTalosContainers lists all containers for a specific Talos cluster.
func (p *Provisioner) listTalosContainers(
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

// collectContainerVolumes collects all volume names used by the given containers.
// It inspects each container to find mounted volumes (anonymous volumes used by Talos).
func (p *Provisioner) collectContainerVolumes(
	ctx context.Context,
	containers []container.Summary,
) []string {
	volumeSet := make(map[string]struct{})

	for _, containerSummary := range containers {
		inspect, err := p.dockerClient.ContainerInspect(ctx, containerSummary.ID)
		if err != nil {
			// Log warning but continue with other containers
			_, _ = fmt.Fprintf(
				p.logWriter,
				"Warning: failed to inspect container %s: %v\n",
				containerSummary.ID[:12],
				err,
			)

			continue
		}

		for _, mount := range inspect.Mounts {
			// Only collect volume mounts (not bind mounts or tmpfs)
			if mount.Type == "volume" && mount.Name != "" {
				volumeSet[mount.Name] = struct{}{}
			}
		}
	}

	volumes := make([]string, 0, len(volumeSet))
	for vol := range volumeSet {
		volumes = append(volumes, vol)
	}

	return volumes
}

// removeVolumes removes the specified volumes.
// Errors are logged but do not cause the operation to fail.
func (p *Provisioner) removeVolumes(ctx context.Context, volumes []string) {
	for _, vol := range volumes {
		err := p.dockerClient.VolumeRemove(ctx, vol, true) // force=true
		if err != nil {
			_, _ = fmt.Fprintf(
				p.logWriter,
				"Warning: failed to remove volume %s: %v\n",
				vol,
				err,
			)
		}
	}
}

// validateClusterOperation validates that Docker is available and the cluster exists.
// Returns the resolved cluster name or an error.
func (p *Provisioner) validateClusterOperation(
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
		return "", fmt.Errorf("%w: %s", clustererr.ErrClusterNotFound, clusterName)
	}

	return clusterName, nil
}

// getClusterContainers validates the operation and returns the cluster's containers.
// This combines validation with container listing for Start/Stop operations.
func (p *Provisioner) getClusterContainers(
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
func (p *Provisioner) bootstrapAndSaveKubeconfig(
	ctx context.Context,
	cluster provision.Cluster,
	configBundle *bundle.Bundle,
) error {
	// Configure endpoints for Docker-in-VM environments (macOS, Windows)
	talosConfig, kubernetesEndpoint, err := p.setupClusterEndpoints(ctx, cluster, configBundle)
	if err != nil {
		return err
	}

	// Create cluster access adapter
	clusterAccess, err := p.createClusterAccess(cluster, talosConfig, kubernetesEndpoint)
	if err != nil {
		return err
	}
	defer func() { _ = clusterAccess.Close() }()

	// Bootstrap cluster and wait for readiness
	err = p.bootstrapAndWaitForReady(ctx, clusterAccess)
	if err != nil {
		return err
	}

	// Fetch and save kubeconfig
	return p.fetchAndSaveKubeconfig(ctx, clusterAccess)
}

// setupClusterEndpoints configures Talos and Kubernetes API endpoints for Docker-in-VM environments.
// Returns the modified talosconfig and kubernetes endpoint for cluster access.
func (p *Provisioner) setupClusterEndpoints(
	ctx context.Context,
	cluster provision.Cluster,
	configBundle *bundle.Bundle,
) (*bundle.TalosConfig, string, error) {
	// Get the mapped Talos API endpoint for Docker-in-VM environments (macOS, Windows).
	// On these platforms, the container's internal IP is not accessible from the host,
	// so we need to use 127.0.0.1 with the mapped port.
	mappedEndpoint, err := p.getMappedTalosAPIEndpoint(ctx, cluster.Info().ClusterName)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get mapped Talos API endpoint: %w", err)
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
		return nil, "", ErrMissingKubernetesEndpoint
	}

	_, _ = fmt.Fprintf(p.logWriter, "Using Kubernetes API endpoint: %s\n", kubernetesEndpoint)

	return talosConfig, kubernetesEndpoint, nil
}

// createClusterAccess creates an access adapter for cluster operations with configured endpoints.
func (p *Provisioner) createClusterAccess(
	cluster provision.Cluster,
	talosConfig *bundle.TalosConfig,
	kubernetesEndpoint string,
) (*access.Adapter, error) {
	// Create access adapter for cluster operations.
	// WithKubernetesEndpoint sets ForceEndpoint which is used to rewrite the kubeconfig.
	clusterAccess := access.NewAdapter(
		cluster,
		provision.WithTalosConfig(talosConfig),
		provision.WithKubernetesEndpoint(kubernetesEndpoint),
	)

	return clusterAccess, nil
}

// bootstrapAndWaitForReady bootstraps the cluster and waits for all components to be ready.
func (p *Provisioner) bootstrapAndWaitForReady(ctx context.Context, clusterAccess *access.Adapter) error {
	// Bootstrap the cluster
	_, _ = fmt.Fprintf(p.logWriter, "Bootstrapping cluster...\n")

	err := clusterAccess.Bootstrap(ctx, p.logWriter)
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
	// When using a custom CNI (e.g., Cilium), skip CNI-dependent checks (CoreDNS, kube-proxy)
	// because pods cannot start until the CNI is installed.
	clusterChecks := p.clusterReadinessChecks()

	err = check.Wait(checkCtx, clusterAccess, clusterChecks, check.StderrReporter())
	if err != nil {
		return fmt.Errorf("cluster readiness check failed: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "Cluster is ready\n")

	return nil
}

// fetchAndSaveKubeconfig retrieves the kubeconfig from the cluster and writes it to disk.
func (p *Provisioner) fetchAndSaveKubeconfig(ctx context.Context, clusterAccess *access.Adapter) error {
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

	// Write kubeconfig to the configured path
	return p.writeKubeconfig(kubeconfig)
}

// provisionCluster creates the Talos cluster using the SDK.
func (p *Provisioner) provisionCluster(
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
func (p *Provisioner) saveClusterConfigs(
	ctx context.Context,
	cluster provision.Cluster,
	configBundle *bundle.Bundle,
) error {
	// Save talosconfig if path is configured
	if p.options.TalosconfigPath != "" {
		saveErr := p.saveTalosconfig(configBundle)
		if saveErr != nil {
			return fmt.Errorf("failed to save talosconfig: %w", saveErr)
		}
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
func (p *Provisioner) checkDockerAvailable(ctx context.Context) error {
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
func (p *Provisioner) buildClusterRequest(
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
func (p *Provisioner) buildNodeRequests(
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
			Name:   fmt.Sprintf("%s-control-plane-%d", clusterName, nodeIndex+1),
			Type:   machine.TypeControlPlane,
			IPs:    []netip.Addr{nodeIP},
			Config: configBundle.ControlPlane(),
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
			Name:   fmt.Sprintf("%s-worker-%d", clusterName, nodeIndex+1),
			Type:   machine.TypeWorker,
			IPs:    []netip.Addr{nodeIP},
			Config: configBundle.Worker(),
		})
	}

	return nodes, nil
}

// talosAPIPort is the Talos apid service port.
const talosAPIPort = 50000

// getMappedTalosAPIEndpoint finds the control plane container and returns the mapped
// Talos API endpoint. On macOS and other non-Linux systems, Docker runs in a VM,
// so we need to use the mapped port via 127.0.0.1 instead of the container's internal IP.
func (p *Provisioner) getMappedTalosAPIEndpoint(
	ctx context.Context,
	clusterName string,
) (string, error) {
	if p.dockerClient == nil {
		return "", ErrDockerNotAvailable
	}

	// Find the control plane container for this cluster
	containers, err := p.listDockerNodesByRole(ctx, clusterName, RoleControlPlane)
	if err != nil {
		return "", fmt.Errorf("failed to list control-plane containers: %w", err)
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
