package talosprovisioner

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
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

// nodeResult describes a single container operation outcome.
type nodeResult interface {
	nodeName() string
	nodeErr() error
	verb() string               // past-tense for recordAppliedChange ("added", "removed")
	action() string             // imperative for error messages ("add", "remove")
	logLine(role string) string // complete success log line
}

// nodeCreationResult records the outcome of a single container creation attempt.
type nodeCreationResult struct {
	name string
	ip   netip.Addr
	err  error
}

func (r nodeCreationResult) nodeName() string { return r.name }
func (r nodeCreationResult) nodeErr() error   { return r.err }
func (r nodeCreationResult) verb() string     { return "added" }
func (r nodeCreationResult) action() string   { return "add" }

func (r nodeCreationResult) logLine(role string) string {
	return fmt.Sprintf("  ✓ Added %s node %s (IP: %s)\n", role, r.name, r.ip.String())
}

// nodeRemovalResult records the outcome of a single container removal attempt.
type nodeRemovalResult struct {
	name string
	err  error
}

func (r nodeRemovalResult) nodeName() string { return r.name }
func (r nodeRemovalResult) nodeErr() error   { return r.err }
func (r nodeRemovalResult) verb() string     { return "removed" }
func (r nodeRemovalResult) action() string   { return "remove" }

func (r nodeRemovalResult) logLine(role string) string {
	return fmt.Sprintf("  ✓ Removed %s node %s\n", role, r.name)
}

// nodeSpec holds the pre-calculated name and IP for a node to be created.
type nodeSpec struct {
	name string
	ip   netip.Addr
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

	indices := availableDockerNodeIndices(existing, clusterName, role, count)

	cidr, err := netip.ParsePrefix(p.options.NetworkCIDR)
	if err != nil {
		return fmt.Errorf("invalid network CIDR: %w", err)
	}

	config := p.configForRole(role)
	if config == nil {
		return fmt.Errorf("%w: %s", ErrNoConfigForRole, role)
	}

	// For worker nodes, fetch the control-plane count once upfront to avoid
	// querying the Docker API on every loop iteration during IP calculation.
	cpCount := p.options.ControlPlaneNodes

	if role != RoleControlPlane {
		cpNodes, countErr := p.countDockerRole(
			ctx, clusterName, RoleControlPlane,
		)
		if countErr == nil {
			cpCount = cpNodes
		}
	}

	// Pre-calculate all node names and IPs sequentially before parallelizing creation.
	specs, err := preCalculateNodeSpecs(
		cidr, clusterName, role, indices, cpCount,
	)
	if err != nil {
		return err
	}

	results, err := p.createNodesInParallel(ctx, clusterName, role, specs, config)
	if err != nil {
		return err
	}

	return p.recordAndWaitForNewDockerNodes(ctx, results, specs, role, result)
}

// recordAndWaitForNewDockerNodes records each container-creation outcome, then —
// when every container was created successfully — blocks until the new nodes'
// Talos APIs are reachable. The reachability wait is the Docker-side analog of
// the Hetzner waitForServersToBeReachable scale-up guard: ContainerStart returns
// before apid is listening inside the container, so without it the update's
// subsequent in-place config reconciliation (fetchNodeConfig) races the boot and
// a just-started node yields "connection refused", recorded as a spurious failed
// change.
func (p *Provisioner) recordAndWaitForNewDockerNodes(
	ctx context.Context,
	results []nodeResult,
	specs []nodeSpec,
	role string,
	result *clusterupdate.UpdateResult,
) error {
	collectErr := p.collectResults(results, role, result)
	if collectErr != nil {
		// A container that failed to create won't become reachable; the failure
		// is already recorded, so surface it without waiting.
		return collectErr
	}

	return p.waitForNewDockerNodesReachable(ctx, specs)
}

// waitForNewDockerNodesReachable blocks until every newly-created node's Talos
// API (apid) is accepting connections, polling each node's container IP in
// parallel (bounded by maxConcurrentContainerOps). It is a no-op for an empty
// slice. This is the Docker-side analog of waitForServersToBeReachable; see
// recordAndWaitForNewDockerNodes for why the wait is needed.
func (p *Provisioner) waitForNewDockerNodesReachable(
	ctx context.Context,
	specs []nodeSpec,
) error {
	if len(specs) == 0 {
		return nil
	}

	// Use the errgroup's derived context so the first failed probe cancels the
	// siblings still waiting, rather than letting them run out their own timeout.
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(maxConcurrentContainerOps)

	for _, spec := range specs {
		group.Go(func() error {
			nodeIP := spec.ip.String()
			p.logf("  Waiting for Talos API on %s (%s)...\n", spec.name, nodeIP)

			checkErr := p.nodeReachabilityCheck(groupCtx, nodeIP)
			if checkErr != nil {
				return fmt.Errorf("waiting for Talos API on %s: %w", spec.name, checkErr)
			}

			p.logf("  ✓ Talos API reachable on %s\n", spec.name)

			return nil
		})
	}

	return group.Wait() //nolint:wrapcheck // per-node errors are wrapped in the closure above
}

// preCalculateNodeSpecs determines the name and static IP for each node to be
// created, one per (0-based) index in indices. Indices may be non-contiguous when
// reclaiming freed slots (see availableDockerNodeIndices), so each node's name and
// IP are derived from its own index rather than a running offset.
func preCalculateNodeSpecs(
	cidr netip.Prefix,
	clusterName, role string,
	indices []int,
	cpCount int,
) ([]nodeSpec, error) {
	specs := make([]nodeSpec, len(indices))

	for idx, nodeIndex := range indices {
		nodeName := dockerNodeName(clusterName, role, nodeIndex)

		nodeIP, ipErr := calculateNodeIP(cidr, role, nodeIndex, cpCount)
		if ipErr != nil {
			return nil, fmt.Errorf("failed to calculate IP for %s: %w", nodeName, ipErr)
		}

		specs[idx] = nodeSpec{name: nodeName, ip: nodeIP}
	}

	return specs, nil
}

// createNodesInParallel creates Talos containers for each spec in parallel
// (up to maxConcurrentContainerOps at a time) and collects the results.
func (p *Provisioner) createNodesInParallel(
	ctx context.Context,
	clusterName, role string,
	specs []nodeSpec,
	config talosconfig.Provider,
) ([]nodeResult, error) {
	results := make([]nodeResult, len(specs))

	group, _ := errgroup.WithContext(ctx)
	group.SetLimit(maxConcurrentContainerOps)

	for idx, spec := range specs {
		group.Go(func() error {
			createErr := p.createTalosContainer(ctx, clusterName, spec.name, role, spec.ip, config)
			results[idx] = nodeCreationResult{name: spec.name, ip: spec.ip, err: createErr}

			return nil // errors collected in results; don't cancel sibling goroutines
		})
	}

	waitErr := group.Wait()
	if waitErr != nil {
		return nil, fmt.Errorf(
			"unexpected error during Talos node creation: %w",
			waitErr,
		)
	}

	return results, nil
}

// removeDockerNodes removes nodes of the given role.
// For control-plane nodes, etcd membership is cleaned up and containers are removed
// sequentially (highest-index first, since etcd safety requires ordered operations).
// For worker nodes, containers are removed in parallel (up to maxConcurrentContainerOps
// at a time) without guaranteed ordering.
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

	if role == RoleControlPlane {
		return p.removeControlPlaneNodesSequentially(
			ctx, clusterName, existing, count, result,
		)
	}

	// Worker nodes have no etcd dependency and can be removed in parallel.
	toRemove := existing[len(existing)-count:]
	results := make([]nodeResult, len(toRemove))

	group, _ := errgroup.WithContext(ctx)
	group.SetLimit(maxConcurrentContainerOps)

	for idx, ctr := range toRemove {
		nodeName := containerName(ctr)

		group.Go(func() error {
			removeErr := p.removeDockerContainer(ctx, ctr.ID)
			results[idx] = nodeRemovalResult{name: nodeName, err: removeErr}

			return nil // errors collected in results; don't cancel sibling goroutines
		})
	}

	waitErr := group.Wait()
	if waitErr != nil {
		return fmt.Errorf(
			"unexpected error during Talos node removal: %w",
			waitErr,
		)
	}

	return p.collectResults(results, role, result)
}

// removeControlPlaneNodesSequentially removes control-plane nodes one at a time,
// cleaning up etcd membership before each removal (highest-index first).
func (p *Provisioner) removeControlPlaneNodesSequentially(
	ctx context.Context,
	clusterName string,
	existing []container.Summary,
	count int,
	result *clusterupdate.UpdateResult,
) error {
	for idx := len(existing) - 1; idx >= len(existing)-count; idx-- {
		ctr := existing[idx]
		nodeName := containerName(ctr)
		nodeIP := containerIP(ctr, clusterName)
		p.etcdCleanupBeforeRemoval(ctx, nodeIP)

		removeErr := p.removeDockerContainer(ctx, ctr.ID)
		if removeErr != nil {
			recordFailedChange(result, RoleControlPlane, nodeName, removeErr)

			return fmt.Errorf(
				"failed to remove control-plane node %s: %w",
				nodeName, removeErr,
			)
		}

		recordAppliedChange(result, RoleControlPlane, nodeName, "removed")

		_, _ = fmt.Fprintf(
			p.logWriter, "  ✓ Removed %s node %s\n",
			RoleControlPlane, nodeName,
		)
	}

	return nil
}

// collectResults records operation outcomes and returns the first error encountered.
func (p *Provisioner) collectResults(
	results []nodeResult,
	role string,
	updateResult *clusterupdate.UpdateResult,
) error {
	var firstErr error

	for _, res := range results {
		if res.nodeErr() != nil {
			recordFailedChange(updateResult, role, res.nodeName(), res.nodeErr())

			if firstErr == nil {
				firstErr = fmt.Errorf(
					"failed to %s %s node %s: %w",
					res.action(), role, res.nodeName(), res.nodeErr(),
				)
			}
		} else {
			recordAppliedChange(updateResult, role, res.nodeName(), res.verb())

			_, _ = fmt.Fprint(p.logWriter, res.logLine(role))
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
			LabelTalosOwned:       labelValueTrue,
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
			filters.Arg("label", LabelTalosOwned+"="+labelValueTrue),
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
// Control-plane nodes start at offset 2 from the network base (cpCount is ignored).
// Worker nodes start after all control-plane slots (cpCount must be pre-fetched by the caller).
func calculateNodeIP(cidr netip.Prefix, role string, nodeIndex, cpCount int) (netip.Addr, error) {
	if role == RoleControlPlane {
		return nthIPInNetwork(cidr, nodeIndex+ipv4Offset)
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

// availableDockerNodeIndices returns the next `count` 0-based indices for a node
// role, reclaiming the lowest freed slot first so a recreated node reuses a removed
// node's name and static IP rather than climbing to max+1 (#5312).
func availableDockerNodeIndices(
	containers []container.Summary,
	clusterName, role string,
	count int,
) []int {
	// Build the name prefix used by dockerNodeName (e.g. "mycluster-controlplane-").
	// dockerNodeName(clusterName, role, 0) returns "<clusterName>-<talosRole>-1";
	// trimming the last character gives the base prefix without the index digit.
	baseName := dockerNodeName(clusterName, role, 0)
	prefix := baseName[:len(baseName)-1]

	names := make([]string, len(containers))
	for i, ctr := range containers {
		names[i] = containerName(ctr)
	}

	// availableNodeIndices returns 1-based suffixes; dockerNodeName and
	// calculateNodeIP expect 0-based indexes (they apply +1 internally), so
	// convert each: index = suffix - 1.
	indices := availableNodeIndices(names, prefix, count)
	for i := range indices {
		indices[i]--
	}

	return indices
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
