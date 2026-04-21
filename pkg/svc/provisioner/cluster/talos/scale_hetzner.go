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

	group, _ := errgroup.WithContext(ctx)
	group.SetLimit(maxConcurrentHetznerOps)

	for nodeIdx := range count {
		group.Go(func() error {
			nodeNumber := nextIndex + nodeIdx
			nodeName := fmt.Sprintf("%s-%s-%d", clusterName, role, nodeNumber)

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
			}, retryOpts)

			results[nodeIdx] = hetznerNodeCreationResult{
				name:   nodeName,
				server: server,
				err:    createErr,
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

// configureNewHetznerNodes waits for Talos API on new servers and applies config.
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
			serverIP := server.PublicNet.IPv4.IP.String()
			p.etcdCleanupBeforeRemoval(ctx, serverIP)
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
