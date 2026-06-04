package talosprovisioner

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"golang.org/x/sync/errgroup"
)

// scaleHetznerByRole adjusts the number of Hetzner servers for the given role.
// Scale-up: creates new servers, waits for Talos API, applies config.
// Scale-down: removes etcd members (for control-plane) then deletes servers (highest-index first).
func (p *Provisioner) scaleHetznerByRole(
	ctx context.Context,
	clusterName, role string,
	delta int,
	result *clusterupdate.UpdateResult,
) error {
	if delta > 0 {
		return p.addHetznerNodes(ctx, clusterName, role, delta, result)
	}

	return p.removeHetznerNodes(ctx, clusterName, role, -delta, result)
}

// addHetznerNodes creates new Hetzner servers for the given role.
// It reuses the existing createHetznerNodes flow, then applies Talos config
// to the newly created servers.
func (p *Provisioner) addHetznerNodes(
	ctx context.Context,
	clusterName, role string,
	count int,
	result *clusterupdate.UpdateResult,
) error {
	hzProvider, existing, err := p.hetznerNodesForRole(ctx, clusterName, role)
	if err != nil {
		return err
	}

	nextIndex := nextHetznerNodeIndex(existing, clusterName, role)

	// Verify server type availability before creating infrastructure
	err = p.checkHetznerAvailabilityForRole(ctx, hzProvider, role)
	if err != nil {
		return err
	}

	infra, err := p.ensureHetznerInfra(ctx, hzProvider, clusterName)
	if err != nil {
		return fmt.Errorf("failed to ensure Hetzner infrastructure: %w", err)
	}

	creationResults, err := p.launchHetznerScaleCreation(
		ctx, hzProvider, clusterName, role, infra, p.hetznerRetryOpts(), nextIndex, count,
	)
	if err != nil {
		return err
	}

	// Record all creation failures before processing so recordFailedChange is sequential.
	for _, res := range creationResults {
		if res.err != nil {
			recordFailedChange(result, role, res.name, res.err)
		}
	}

	servers, err := p.collectCreatedHetznerServers(creationResults, role)
	if err != nil {
		return err
	}

	err = p.configureNewHetznerNodes(ctx, servers, role, result)
	if err != nil {
		return err
	}

	for _, server := range servers {
		recordAppliedChange(result, role, server.Name, "added")
	}

	return nil
}

// launchHetznerScaleCreation creates count Hetzner servers starting at nextIndex, in parallel.
// Results are returned indexed — goroutines always return nil so group.Wait() only fails on
// unexpected errgroup-level errors.
func (p *Provisioner) launchHetznerScaleCreation(
	ctx context.Context,
	hzProvider *hetzner.Provider,
	clusterName, role string,
	infra HetznerInfra,
	retryOpts hetzner.ServerRetryOpts,
	nextIndex, count int,
) ([]hetznerNodeCreationResult, error) {
	results := make([]hetznerNodeCreationResult, count)

	enableIPv4, enableIPv6 := p.hetznerPublicNetForRole(role)

	group, _ := errgroup.WithContext(ctx)
	group.SetLimit(maxConcurrentHetznerOps)

	for nodeIdx := range count {
		group.Go(func() error {
			nodeNumber := nextIndex + nodeIdx

			nodeName, nameErr := hetznerNodeName(clusterName, role, nodeNumber)
			if nameErr != nil {
				// Validation failure: record it and skip provisioning (no billable
				// server created). The goroutine still returns nil; the error is
				// surfaced when addHetznerNodes reads results.
				results[nodeIdx] = hetznerNodeCreationResult{name: nodeName, err: nameErr}
			} else {
				server, createErr := hzProvider.CreateServerWithRetry(ctx, hetzner.CreateServerOpts{
					Name:             nodeName,
					ServerType:       p.hetznerServerType(role),
					ISOID:            p.talosOpts.ISO,
					Location:         p.hetznerOpts.Location,
					Labels:           hetzner.NodeLabels(clusterName, role, nodeNumber),
					NetworkID:        infra.NetworkID,
					PlacementGroupID: infra.PlacementGroupID,
					SSHKeyID:         infra.SSHKeyID,
					FirewallIDs:      []int64{infra.FirewallID},
					EnableIPv4:       enableIPv4,
					EnableIPv6:       enableIPv6,
				}, retryOpts)

				results[nodeIdx] = hetznerNodeCreationResult{
					name:   nodeName,
					server: server,
					err:    createErr,
				}
			}

			return nil // errors collected in results
		})
	}

	waitErr := group.Wait()
	if waitErr != nil {
		return nil, fmt.Errorf("unexpected error during Hetzner scale-up: %w", waitErr)
	}

	return results, nil
}

// configureNewHetznerNodes waits for the Talos API on new servers, applies their
// role config, then blocks until the nodes finish installing and reboot. Applying
// config triggers a Talos install-to-disk and automatic reboot during which the
// Talos API is unreachable; waiting before returning keeps scale-up consistent with
// initial create and rolling replace, and prevents the in-place config
// reconciliation that runs next in Update from racing the reboot (the cause of the
// spurious "connection refused" failure when fetching a just-created node's config).
func (p *Provisioner) configureNewHetznerNodes(
	ctx context.Context,
	servers []*hcloud.Server,
	role string,
	result *clusterupdate.UpdateResult,
) error {
	if len(servers) == 0 {
		return nil
	}

	_, _ = fmt.Fprintf(p.logWriter, "  Waiting for Talos API on %d new %s node(s)...\n",
		len(servers), role)

	err := p.waitForHetznerTalosAPI(ctx, servers)
	if err != nil {
		return fmt.Errorf("failed waiting for Talos API on new nodes: %w", err)
	}

	config := p.configForRole(role)
	if config == nil {
		return fmt.Errorf("%w: %s", ErrNoConfigForRole, role)
	}

	// Collect per-node apply results so recordFailedChange is called sequentially after
	// the parallel apply completes, avoiding concurrent appends to result.FailedChanges.
	type applyResult struct {
		serverName string
		err        error
	}

	applyResults := make([]applyResult, len(servers))

	group, _ := errgroup.WithContext(ctx)
	group.SetLimit(maxConcurrentHetznerOps)

	for idx, server := range servers {
		group.Go(func() error {
			applyErr := p.applyConfigToNode(ctx, server, config)
			applyResults[idx] = applyResult{serverName: server.Name, err: applyErr}

			return nil // errors collected in applyResults
		})
	}

	waitErr := group.Wait()
	if waitErr != nil {
		return fmt.Errorf("unexpected error during config apply: %w", waitErr)
	}

	for _, res := range applyResults {
		if res.err != nil {
			recordFailedChange(result, role, res.serverName, res.err)

			return fmt.Errorf("failed to apply config to %s: %w", res.serverName, res.err)
		}
	}

	// The config just applied triggers a Talos install-to-disk and an automatic
	// reboot on each new node; block until they come back so the in-place config
	// reconciliation that runs next in Update does not race the reboot.
	return p.waitForNewHetznerNodesReachable(ctx, servers, role)
}

// waitForNewHetznerNodesReachable blocks until newly created nodes finish
// installing Talos to disk and reboot, so their Talos API is reachable again.
//
// Applying machine config to a freshly created node triggers an install and an
// automatic reboot; during that window the node's Talos API refuses connections.
// Returning before the nodes recover lets the in-place config reconciliation that
// runs next in Update (applyInPlaceConfigChanges) race the reboot and record a
// spurious "connection refused" failure when it fetches the new node's running
// config. Mirrors configureAndWaitReplacement (rolling replace) and
// detachOrWaitForReboot (initial create), which already wait for this reboot.
func (p *Provisioner) waitForNewHetznerNodesReachable(
	ctx context.Context,
	servers []*hcloud.Server,
	role string,
) error {
	_, _ = fmt.Fprintf(p.logWriter,
		"  Waiting for %d new %s node(s) to install and reboot...\n", len(servers), role)

	err := p.waitForServersToBeReachable(ctx, servers)
	if err != nil {
		return fmt.Errorf("waiting for new %s node(s) to become reachable: %w", role, err)
	}

	return nil
}

// removeHetznerNodes removes Hetzner servers for a given role (highest-index first).
// For control-plane nodes, etcd membership is cleaned up before each removal.
func (p *Provisioner) removeHetznerNodes(
	ctx context.Context,
	clusterName, role string,
	count int,
	result *clusterupdate.UpdateResult,
) error {
	hzProvider, existing, err := p.hetznerNodesForRole(ctx, clusterName, role)
	if err != nil {
		return err
	}

	if count > len(existing) {
		count = len(existing)
	}

	for i := len(existing) - 1; i >= len(existing)-count; i-- {
		server := existing[i]

		// Best-effort etcd cleanup for control-plane nodes
		if role == RoleControlPlane {
			serverIP, addrErr := hetznerNodeTalosAddress(server)
			if addrErr == nil {
				p.etcdCleanupBeforeRemoval(ctx, serverIP)
			}
		}

		err = p.deleteHetznerServer(ctx, hzProvider, server)
		if err != nil {
			recordFailedChange(result, role, server.Name, err)

			return fmt.Errorf("failed to delete %s server %s: %w", role, server.Name, err)
		}

		recordAppliedChange(result, role, server.Name, "removed")

		_, _ = fmt.Fprintf(p.logWriter, "  ✓ Removed %s server %s\n", role, server.Name)
	}

	return nil
}

// listHetznerNodesByRole returns servers for a cluster filtered by role, sorted by name.
func (p *Provisioner) listHetznerNodesByRole(
	ctx context.Context,
	hzProvider *hetzner.Provider,
	clusterName, role string,
) ([]*hcloud.Server, error) {
	nodes, err := hzProvider.ListNodes(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	var servers []*hcloud.Server

	for _, node := range nodes {
		if node.Role != role {
			continue
		}

		server, serverErr := hzProvider.GetServerByName(ctx, node.Name)
		if serverErr != nil {
			return nil, fmt.Errorf("failed to get server %s: %w", node.Name, serverErr)
		}

		if server != nil {
			servers = append(servers, server)
		}
	}

	slices.SortFunc(servers, func(a, b *hcloud.Server) int {
		return strings.Compare(a.Name, b.Name)
	})

	return servers, nil
}

// nextHetznerNodeIndex finds the next available index for a node role.
// It scans existing server names to find the max index, avoiding naming collisions
// when there are gaps in the index sequence (e.g., nodes 1,2,4 after removing 3).
func nextHetznerNodeIndex(servers []*hcloud.Server, clusterName, role string) int {
	prefix := fmt.Sprintf("%s-%s-", clusterName, role)

	names := make([]string, len(servers))
	for i, server := range servers {
		names[i] = server.Name
	}

	return nextNodeIndexFromNames(names, prefix)
}

// deleteHetznerServer deletes a single Hetzner Cloud server.
func (p *Provisioner) deleteHetznerServer(
	ctx context.Context,
	hzProvider *hetzner.Provider,
	server *hcloud.Server,
) error {
	_, _ = fmt.Fprintf(p.logWriter, "  Deleting server %s...\n", server.Name)

	err := hzProvider.DeleteServer(ctx, server)
	if err != nil {
		return fmt.Errorf("failed to delete server %s: %w", server.Name, err)
	}

	return nil
}

// hetznerProvider extracts the Hetzner provider from the infra provider.
func (p *Provisioner) hetznerProvider() (*hetzner.Provider, error) {
	hzProvider, ok := p.infraProvider.(*hetzner.Provider)
	if !ok {
		return nil, fmt.Errorf("%w: got %T", ErrHetznerProviderRequired, p.infraProvider)
	}

	return hzProvider, nil
}

// hetznerServerType returns the server type for a given role.
func (p *Provisioner) hetznerServerType(role string) string {
	if p.hetznerOpts == nil {
		return ""
	}

	if role == RoleControlPlane {
		return p.hetznerOpts.ControlPlaneServerType
	}

	return p.hetznerOpts.WorkerServerType
}

// hetznerPublicNetForRole returns the public IPv4/IPv6 toggles for the given role as
// *bool values suitable for hetzner.CreateServerOpts. Nil toggles (the result when no
// Hetzner options are configured) default to a public IP, matching Hetzner's behavior.
func (p *Provisioner) hetznerPublicNetForRole(role string) (*bool, *bool) {
	if p.hetznerOpts == nil {
		return nil, nil
	}

	ipv4 := p.hetznerOpts.WorkerIPv4Enabled()
	ipv6 := p.hetznerOpts.WorkerIPv6Enabled()

	if role == RoleControlPlane {
		ipv4 = p.hetznerOpts.ControlPlaneIPv4Enabled()
		ipv6 = p.hetznerOpts.ControlPlaneIPv6Enabled()
	}

	return &ipv4, &ipv6
}

// hetznerRetryOpts builds retry options from Hetzner configuration.
func (p *Provisioner) hetznerRetryOpts() hetzner.ServerRetryOpts {
	opts := hetzner.ServerRetryOpts{
		LogWriter: p.syncLogWriter(),
	}

	if p.hetznerOpts != nil {
		opts.FallbackLocations = p.hetznerOpts.FallbackLocations
		opts.AllowPlacementFallback = p.hetznerOpts.PlacementGroupFallbackToNone
	}

	return opts
}

// checkHetznerAvailabilityForRole verifies that the server type for the given
// role is available in the configured locations. Used during scale-up to fail
// fast before creating infrastructure resources.
func (p *Provisioner) checkHetznerAvailabilityForRole(
	ctx context.Context,
	hzProvider *hetzner.Provider,
	role string,
) error {
	if p.hetznerOpts == nil {
		return nil
	}

	serverType := p.hetznerServerType(role)
	if serverType == "" {
		return nil
	}

	_, _ = fmt.Fprintf(p.logWriter, "Checking server type availability for %s...\n", role)

	err := hzProvider.CheckServerAvailabilityWithRetry(
		ctx,
		[]string{serverType},
		p.hetznerOpts.Location,
		p.hetznerOpts.FallbackLocations,
		hetzner.DefaultMaxAvailabilityCheckRetries,
		p.logWriter,
	)
	if err != nil {
		return fmt.Errorf("server availability check failed for %s: %w", role, err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Server type %q available\n", serverType)

	return nil
}
