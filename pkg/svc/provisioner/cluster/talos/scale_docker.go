package talosprovisioner

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"golang.org/x/sync/errgroup"
)

// maxConcurrentContainerOps caps the number of Docker containers created or removed in parallel.
// A value of 3 balances throughput and Docker daemon load for both scale-up and worker scale-down.
const maxConcurrentContainerOps = 3

// scaleDockerByRole adjusts the number of Docker nodes for the given role.
// Scale-up: creates new containers with proper Talos config and static IPs.
// Scale-down: removes etcd members (for control-plane) then stops and removes containers (highest-index first).
func (p *Provisioner) scaleDockerByRole(
	ctx context.Context,
	clusterName, role string,
	delta int,
	result *clusterupdate.UpdateResult,
) error {
	if delta > 0 {
		return p.addDockerNodes(ctx, clusterName, role, delta, result)
	}

	return p.removeDockerNodes(ctx, clusterName, role, -delta, result)
}

// nodeCreationResult records the outcome of a single container creation attempt.
type nodeCreationResult struct {
	name string
	ip   netip.Addr
	err  error
}

// nodeRemovalResult records the outcome of a single container removal attempt.
type nodeRemovalResult struct {
	name string
	err  error
}

// addDockerNodes creates new Talos Docker containers for the given role.
// IPs are pre-calculated sequentially (to preserve deterministic address assignment),
// then containers are created in parallel (up to maxConcurrentContainerOps at a time)
// to reduce wall-clock time when adding multiple nodes.
func (p *Provisioner) addDockerNodes(
	ctx context.Context,
	clusterName, role string,
	count int,
	result *clusterupdate.UpdateResult,
) error {
	existing, err := p.listDockerNodesByRole(ctx, clusterName, role)
	if err != nil {
		return fmt.Errorf("failed to list %s nodes: %w", role, err)
	}

	nextIndex := nextDockerNodeIndex(existing, clusterName, role)

	cidr, err := netip.ParsePrefix(p.options.NetworkCIDR)
	if err != nil {
		return fmt.Errorf("invalid network CIDR: %w", err)
	}

	config := p.configForRole(role)
	if config == nil {
		return fmt.Errorf("%w: %s", ErrNoConfigForRole, role)
	}

	// Pre-calculate all node names and IPs sequentially before parallelizing
	// creation. calculateNodeIP may call the Docker API for worker nodes
	// (to count control-plane slots), so we do this once per node upfront.
	type nodeSpec struct {
		name string
		ip   netip.Addr
	}

	specs := make([]nodeSpec, count)

	for i := range count {
		nodeIndex := nextIndex + i
		nodeName := dockerNodeName(clusterName, role, nodeIndex)

		nodeIP, ipErr := p.calculateNodeIP(ctx, cidr, clusterName, role, nodeIndex)
		if ipErr != nil {
			return fmt.Errorf("failed to calculate IP for %s: %w", nodeName, ipErr)
		}

		specs[i] = nodeSpec{name: nodeName, ip: nodeIP}
	}

	// Parallelize container creation. Each goroutine writes to its own slot
	// in the results slice, so no mutex is needed.
	results := make([]nodeCreationResult, count)

	g, _ := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrentContainerOps)

	for i, spec := range specs {
		i, spec := i, spec

		g.Go(func() error {
			createErr := p.createTalosContainer(ctx, clusterName, spec.name, role, spec.ip, config)
			results[i] = nodeCreationResult{name: spec.name, ip: spec.ip, err: createErr}

			return nil // errors collected in results; don't cancel sibling goroutines
		})
	}

	if waitErr := g.Wait(); waitErr != nil {
		// With the current g.Go bodies this should be unreachable, but handle defensively
		// to satisfy errcheck and surface any unexpected errgroup errors.
		return fmt.Errorf("unexpected error while waiting for Talos node creation goroutines: %w", waitErr)
	}

	// Record applied/failed changes and surface the first error encountered.
	var firstErr error

	for _, res := range results {
		if res.err != nil {
			recordFailedChange(result, role, res.name, res.err)

			if firstErr == nil {
				firstErr = fmt.Errorf("failed to create %s node %s: %w", role, res.name, res.err)
			}
		} else {
			recordAppliedChange(result, role, res.name, "added")

			_, _ = fmt.Fprintf(p.logWriter, "  ✓ Added %s node %s (IP: %s)\n",
				role, res.name, res.ip.String())
		}
	}

	return firstErr
}

// removeDockerNodes removes nodes of the given role (highest-index first).
// For control-plane nodes, etcd membership is cleaned up sequentially before each removal
// (etcd safety requires ordered operations). For worker nodes, containers are removed
// in parallel (up to maxConcurrentContainerOps at a time).
func (p *Provisioner) removeDockerNodes(
	ctx context.Context,
	clusterName, role string,
	count int,
	result *clusterupdate.UpdateResult,
) error {
	existing, err := p.listDockerNodesByRole(ctx, clusterName, role)
	if err != nil {
		return fmt.Errorf("listing existing %s nodes for removal: %w", role, err)
	}

	count = min(count, len(existing))

	// Control-plane nodes must be removed sequentially: etcd membership cleanup
	// must run in order before each container is stopped.
	if role == RoleControlPlane {
		for i := len(existing) - 1; i >= len(existing)-count; i-- {
			ctr := existing[i]
			nodeName := containerName(ctr)

			nodeIP := containerIP(ctr, clusterName)
			p.etcdCleanupBeforeRemoval(ctx, nodeIP)

			if removeErr := p.removeDockerContainer(ctx, ctr.ID); removeErr != nil {
				recordFailedChange(result, role, nodeName, removeErr)

				return fmt.Errorf("failed to remove %s node %s: %w", role, nodeName, removeErr)
			}

			recordAppliedChange(result, role, nodeName, "removed")

			_, _ = fmt.Fprintf(p.logWriter, "  ✓ Removed %s node %s\n", role, nodeName)
		}

		return nil
	}

	// Worker nodes have no etcd dependency and can be removed in parallel.
	toRemove := existing[len(existing)-count:]
	results := make([]nodeRemovalResult, len(toRemove))

	g, _ := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrentContainerOps)

	for i, ctr := range toRemove {
		i, ctr := i, ctr
		nodeName := containerName(ctr)

		g.Go(func() error {
			removeErr := p.removeDockerContainer(ctx, ctr.ID)
			results[i] = nodeRemovalResult{name: nodeName, err: removeErr}

			return nil // errors collected in results; don't cancel sibling goroutines
		})
	}

	if waitErr := g.Wait(); waitErr != nil {
		// With the current g.Go bodies this should be unreachable, but handle defensively
		// to satisfy errcheck and surface any unexpected errgroup errors.
		return fmt.Errorf("unexpected error while waiting for Talos node removal goroutines: %w", waitErr)
	}

	// Record applied/failed changes and surface the first error encountered.
	var firstErr error

	for _, res := range results {
		if res.err != nil {
			recordFailedChange(result, role, res.name, res.err)

			if firstErr == nil {
				firstErr = fmt.Errorf("failed to remove %s node %s: %w", role, res.name, res.err)
			}
		} else {
			recordAppliedChange(result, role, res.name, "removed")

			_, _ = fmt.Fprintf(p.logWriter, "  ✓ Removed %s node %s\n", role, res.name)
		}
	}

	return firstErr
}

// createTalosContainer creates and starts a Docker container matching the
// Talos SDK's container spec. This includes: privileged mode, PLATFORM=container
// env, USERDATA with base64 config, Talos labels, tmpfs mounts, anonymous
// volumes, seccomp:unconfined, and a static IP on the cluster network.
func (p *Provisioner) createTalosContainer(
	ctx context.Context,
	clusterName, nodeName, role string,
	nodeIP netip.Addr,
	config talosconfig.Provider,
) error {
	cfgStr, err := config.EncodeString()
	if err != nil {
		return fmt.Errorf("failed to encode config: %w", err)
	}

	env := []string{
		"PLATFORM=container",
		"USERDATA=" + base64.StdEncoding.EncodeToString([]byte(cfgStr)),
	}

	containerConfig := &container.Config{
		Hostname: nodeName,
		Image:    p.options.TalosImage,
		Env:      env,
		Labels: map[string]string{
			LabelTalosOwned:       "true",
			LabelTalosClusterName: clusterName,
			"talos.type":          talosTypeFromRole(role),
		},
	}

	hostConfig := buildTalosHostConfig()

	networkConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			clusterName: {
				NetworkID: clusterName,
				IPAMConfig: &network.EndpointIPAMConfig{
					IPv4Address: nodeIP.String(),
				},
			},
		},
	}

	resp, err := p.dockerClient.ContainerCreate(ctx, containerConfig, hostConfig,
		networkConfig, nil, nodeName)
	if err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}

	err = p.dockerClient.ContainerStart(ctx, resp.ID, container.StartOptions{})
	if err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	return nil
}

// removeDockerContainer stops and removes a container and its volumes.
func (p *Provisioner) removeDockerContainer(ctx context.Context, containerID string) error {
	timeout := containerStopTimeout

	_ = p.dockerClient.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeout})

	err := p.dockerClient.ContainerRemove(ctx, containerID, container.RemoveOptions{
		Force:         true,
		RemoveVolumes: true,
	})
	if err != nil {
		return fmt.Errorf("failed to remove container: %w", err)
	}

	return nil
}

// buildTalosHostConfig creates the HostConfig matching the Talos SDK's spec:
// privileged, seccomp:unconfined, readonly rootfs, tmpfs for /run /system /tmp,
// anonymous volumes for data paths, and resource limits.
func buildTalosHostConfig() *container.HostConfig {
	mounts := make([]mount.Mount, 0, len(constants.Overlays)+5) //nolint:mnd

	// Tmpfs mounts for /run, /system, /tmp
	for _, path := range []string{"/run", "/system", "/tmp"} {
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeTmpfs,
			Target: path,
		})
	}

	// Anonymous volumes for persistent data
	volumePaths := make([]string, 0, len(constants.Overlays)+2) //nolint:mnd
	volumePaths = append(volumePaths, constants.EphemeralMountPoint, constants.StateMountPoint)

	for _, overlay := range constants.Overlays {
		volumePaths = append(volumePaths, overlay.Path)
	}

	for _, path := range volumePaths {
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeVolume,
			Target: path,
		})
	}

	return &container.HostConfig{
		Privileged:     true,
		SecurityOpt:    []string{"seccomp:unconfined"},
		ReadonlyRootfs: true,
		Mounts:         mounts,
	}
}

// listDockerNodesByRole lists containers for a specific role, sorted by name.
func (p *Provisioner) listDockerNodesByRole(
	ctx context.Context,
	clusterName, role string,
) ([]container.Summary, error) {
	containers, err := p.dockerClient.ContainerList(ctx, container.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", LabelTalosOwned+"=true"),
			filters.Arg("label", LabelTalosClusterName+"="+clusterName),
			filters.Arg("label", "talos.type="+talosTypeFromRole(role)),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	slices.SortFunc(containers, func(a, b container.Summary) int {
		return strings.Compare(containerName(a), containerName(b))
	})

	return containers, nil
}

// calculateNodeIP determines the static IP for a new node based on its role and index.
// Control-plane nodes start at offset 2 from the network base.
// Worker nodes start after all control-plane slots.
func (p *Provisioner) calculateNodeIP(
	ctx context.Context,
	cidr netip.Prefix,
	clusterName, role string,
	nodeIndex int,
) (netip.Addr, error) {
	if role == RoleControlPlane {
		return nthIPInNetwork(cidr, nodeIndex+ipv4Offset)
	}

	// Workers start after the maximum control-plane count
	cpCount, err := p.countDockerRole(ctx, clusterName, RoleControlPlane)
	if err != nil {
		// Fall back to configured count
		cpCount = p.options.ControlPlaneNodes
	}

	return nthIPInNetwork(cidr, cpCount+nodeIndex+ipv4Offset)
}

// countDockerRole counts running containers for a role.
func (p *Provisioner) countDockerRole(
	ctx context.Context,
	clusterName, role string,
) (int, error) {
	nodes, err := p.listDockerNodesByRole(ctx, clusterName, role)
	if err != nil {
		return 0, err
	}

	return len(nodes), nil
}

// configForRole returns the appropriate Talos config for a role,
// using the nil-safe Configs accessors.
func (p *Provisioner) configForRole(role string) talosconfig.Provider {
	if p.talosConfigs == nil {
		return nil
	}

	if role == RoleControlPlane {
		return p.talosConfigs.ControlPlane()
	}

	return p.talosConfigs.Worker()
}

// nextDockerNodeIndex finds the next available 0-based index for a node role.
// It scans existing container names to find the max suffix, then converts the
// result to the 0-based index expected by dockerNodeName (which applies +1
// internally when formatting names).
func nextDockerNodeIndex(containers []container.Summary, clusterName, role string) int {
	// Build the name prefix used by dockerNodeName (e.g. "mycluster-controlplane-").
	// dockerNodeName(clusterName, role, 0) returns "<clusterName>-<talosRole>-1";
	// trimming the last character gives the base prefix without the index digit.
	baseName := dockerNodeName(clusterName, role, 0)
	prefix := baseName[:len(baseName)-1]

	names := make([]string, len(containers))
	for i, ctr := range containers {
		names[i] = containerName(ctr)
	}

	// nextNodeIndexFromNames returns the next available numeric suffix (1-based).
	// dockerNodeName uses index+1 for the suffix, so convert: index = nextSuffix-1.
	nextSuffix := nextNodeIndexFromNames(names, prefix)

	return nextSuffix - 1
}

// dockerNodeName formats a Docker container name for a Talos node.
func dockerNodeName(clusterName, role string, index int) string {
	talosRole := talosTypeFromRole(role)

	return fmt.Sprintf("%s-%s-%d", clusterName, talosRole, index+1)
}

// talosTypeFromRole converts our generic role name to the Talos label value.
func talosTypeFromRole(role string) string {
	if role == RoleControlPlane {
		return "controlplane"
	}

	return RoleWorker
}

// containerName extracts the container name from a Summary, stripping the leading "/".
func containerName(ctr container.Summary) string {
	if len(ctr.Names) == 0 {
		return ""
	}

	return strings.TrimPrefix(ctr.Names[0], "/")
}

// containerIP extracts the container's IP address on the cluster network.
func containerIP(ctr container.Summary, networkName string) string {
	if ctr.NetworkSettings == nil {
		return ""
	}

	if net, ok := ctr.NetworkSettings.Networks[networkName]; ok {
		return net.IPAddress
	}

	// Fall back to first available network
	for _, net := range ctr.NetworkSettings.Networks {
		if net.IPAddress != "" {
			return net.IPAddress
		}
	}

	return ""
}
