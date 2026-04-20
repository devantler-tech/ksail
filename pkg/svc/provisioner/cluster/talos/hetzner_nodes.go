package talosprovisioner

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/netretry"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/siderolabs/go-retry/retry"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/bundle"
	"golang.org/x/sync/errgroup"
)

// maxConcurrentHetznerOps caps the number of Hetzner API operations executed in parallel.
// A value of 3 balances throughput and API rate-limit headroom.
const maxConcurrentHetznerOps = 3

// Apply-configuration retry defaults for transient Talos API handshake races.
const (
	talosApplyConfigMaxAttempts   = 3
	talosApplyConfigRetryBaseWait = 5 * time.Second
	talosApplyConfigRetryMaxWait  = 20 * time.Second

	// grpcUnavailable is the numeric gRPC status code for Unavailable (14).
	// Using the raw constant avoids importing google.golang.org/grpc directly.
	grpcUnavailable = 14
)

// errRetriesExhausted is returned when all retry attempts for config apply have been used.
var errRetriesExhausted = errors.New("retries exhausted")

// hetznerNodeCreationResult holds the outcome of a single Hetzner server creation attempt.
type hetznerNodeCreationResult struct {
	name   string
	server *hcloud.Server
	err    error
}

// runParallelOnServers runs fn for each server concurrently, bounded by limit concurrent ops.
// Errors from fn should be self-describing (include server name etc.) — this helper adds no
// extra context. Callers in the same package may return its result directly without wrapcheck.
func runParallelOnServers(
	ctx context.Context,
	servers []*hcloud.Server,
	limit int,
	operation func(*hcloud.Server) error,
) error {
	group, _ := errgroup.WithContext(ctx)
	group.SetLimit(limit)

	for _, server := range servers {
		group.Go(func() error {
			return operation(server)
		})
	}

	return group.Wait() //nolint:wrapcheck // errors are wrapped by operation
}

// createHetznerNodes creates a batch of Hetzner servers for a given role (control-plane or worker).
//
// Parameters:
//   - ctx: request-scoped context for cancellation and timeouts.
//   - provider: Hetzner infrastructure provider used to create the servers.
//   - infra: shared infrastructure resources (network, firewall, placement group, SSH key).
//   - opts: node group specification (cluster name, role, count, server type, ISO, location).
//
// Returns:
//   - []*hcloud.Server: slice of successfully created servers (empty if count <= 0).
//   - error: non-nil if any server creation fails.
func (p *Provisioner) createHetznerNodes(
	ctx context.Context,
	hzProvider *hetzner.Provider,
	infra HetznerInfra,
	opts HetznerNodeGroupOpts,
) ([]*hcloud.Server, error) {
	if opts.Count <= 0 {
		return []*hcloud.Server{}, nil
	}

	_, _ = fmt.Fprintf(p.logWriter, "Creating %d %s node(s)...\n", opts.Count, opts.Role)

	retryOpts := hetzner.ServerRetryOpts{LogWriter: p.syncLogWriter()}

	if p.hetznerOpts != nil {
		retryOpts.FallbackLocations = p.hetznerOpts.FallbackLocations
		retryOpts.AllowPlacementFallback = p.hetznerOpts.PlacementGroupFallbackToNone
	}

	results := make([]hetznerNodeCreationResult, opts.Count)

	group, _ := errgroup.WithContext(ctx)
	group.SetLimit(maxConcurrentHetznerOps)

	for nodeIndex := range opts.Count {
		group.Go(func() error {
			nodeName := fmt.Sprintf("%s-%s-%d", opts.ClusterName, opts.Role, nodeIndex+1)

			server, err := hzProvider.CreateServerWithRetry(ctx, hetzner.CreateServerOpts{
				Name:             nodeName,
				ServerType:       opts.ServerType,
				ISOID:            opts.ISOID,
				Location:         opts.Location,
				Labels:           hetzner.NodeLabels(opts.ClusterName, opts.Role, nodeIndex+1),
				NetworkID:        infra.NetworkID,
				PlacementGroupID: infra.PlacementGroupID,
				SSHKeyID:         infra.SSHKeyID,
				FirewallIDs:      []int64{infra.FirewallID},
			}, retryOpts)

			results[nodeIndex] = hetznerNodeCreationResult{name: nodeName, server: server, err: err}

			return nil // errors collected in results
		})
	}

	waitErr := group.Wait()
	if waitErr != nil {
		return nil, fmt.Errorf("unexpected error during Hetzner node creation: %w", waitErr)
	}

	return p.collectCreatedHetznerServers(results, opts.Role)
}

// collectCreatedHetznerServers processes creation results sequentially, logging each success
// and returning the first failure with the node name included in the error.
func (p *Provisioner) collectCreatedHetznerServers(
	results []hetznerNodeCreationResult,
	role string,
) ([]*hcloud.Server, error) {
	servers := make([]*hcloud.Server, 0, len(results))

	for _, res := range results {
		if res.err != nil {
			return nil, fmt.Errorf("failed to create %s node %s: %w", role, res.name, res.err)
		}

		servers = append(servers, res.server)

		_, _ = fmt.Fprintf(
			p.logWriter,
			"  ✓ %s node %s created (IP: %s)\n",
			role,
			res.name,
			res.server.PublicNet.IPv4.IP.String(),
		)
	}

	return servers, nil
}

// hetznerNodesForRole returns the Hetzner provider and existing servers for the given cluster role.
func (p *Provisioner) hetznerNodesForRole(
	ctx context.Context,
	clusterName, role string,
) (*hetzner.Provider, []*hcloud.Server, error) {
	hzProvider, err := p.hetznerProvider()
	if err != nil {
		return nil, nil, err
	}

	existing, err := p.listHetznerNodesByRole(ctx, hzProvider, clusterName, role)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list %s nodes: %w", role, err)
	}

	return hzProvider, existing, nil
}

// waitForHetznerTalosAPI waits for the Talos API to be reachable on all Hetzner servers.
// Nodes booted from ISO are in maintenance mode and expose the Talos API on port 50000.
func (p *Provisioner) waitForHetznerTalosAPI(
	ctx context.Context,
	servers []*hcloud.Server,
) error {
	return runParallelOnServers(ctx, servers, len(servers), func(server *hcloud.Server) error {
		serverIP := server.PublicNet.IPv4.IP.String()
		endpoint := fmt.Sprintf("%s:%d", serverIP, talosAPIPort)

		p.logf("  Waiting for Talos API on %s (%s)...\n", server.Name, endpoint)

		err := retry.Constant(talosAPIWaitTimeout, retry.WithUnits(retryInterval)).
			RetryWithContext(ctx, func(ctx context.Context) error {
				// Try to establish a TLS connection to verify the Talos API is responding.
				// In maintenance mode the node has no certificate yet, so TLS verification
				// must be skipped. Most APIs return "not implemented" which is expected.
				retryClient, connErr := talosclient.New(ctx,
					talosclient.WithEndpoints(serverIP),
					talosclient.WithTLSConfig(&tls.Config{
						InsecureSkipVerify: true, //nolint:gosec // Maintenance mode: no cert yet
					}),
				)
				if connErr != nil {
					return retry.ExpectedError(connErr)
				}

				defer retryClient.Close() //nolint:errcheck

				// "Unimplemented" response means the API is reachable in maintenance mode —
				// the node is ready for config application.
				_, versionErr := retryClient.Version(ctx)
				if versionErr != nil {
					if strings.Contains(versionErr.Error(), "Unimplemented") {
						return nil
					}

					return retry.ExpectedError(versionErr)
				}

				return nil
			})
		if err != nil {
			return fmt.Errorf("timeout waiting for Talos API on %s: %w", server.Name, err)
		}

		p.logf("  ✓ Talos API reachable on %s\n", server.Name)

		return nil
	})
}

// applyHetznerConfigs applies machine configuration to all Hetzner nodes in parallel.
// It uses the insecure Talos client to connect to nodes in maintenance mode.
func (p *Provisioner) applyHetznerConfigs(
	ctx context.Context,
	_ string,
	controlPlaneServers []*hcloud.Server,
	workerServers []*hcloud.Server,
	configBundle *bundle.Bundle,
) error {
	cpConfig := configBundle.ControlPlane()
	workerConfig := configBundle.Worker()

	allServers := make([]*hcloud.Server, 0, len(controlPlaneServers)+len(workerServers))
	configs := make([]talosconfig.Provider, 0, len(controlPlaneServers)+len(workerServers))

	for _, server := range controlPlaneServers {
		allServers = append(allServers, server)
		configs = append(configs, cpConfig)
	}

	for _, server := range workerServers {
		allServers = append(allServers, server)
		configs = append(configs, workerConfig)
	}

	group, _ := errgroup.WithContext(ctx)
	group.SetLimit(maxConcurrentHetznerOps)

	for idx, server := range allServers {
		cfg := configs[idx]

		group.Go(func() error {
			err := p.applyConfigToNode(ctx, server, cfg)
			if err != nil {
				return fmt.Errorf("failed to apply config to %s: %w", server.Name, err)
			}

			return nil
		})
	}

	err := group.Wait()
	if err != nil {
		return fmt.Errorf("applying configs to Hetzner nodes: %w", err)
	}

	return nil
}

// detachISOsAndReboot handles the post-config-apply phase of Hetzner Talos installation.
//
// After ApplyConfiguration, Talos runs the install sequence which:
// 1. Installs Talos to disk (creates STATE, EPHEMERAL partitions)
// 2. Automatically reboots the server
//
// On Hetzner, after reboot with an installed disk, the server typically boots from disk
// even with ISO still attached (disk gets higher boot priority after install).
//
// This function:
// 1. Waits for the installation + automatic reboot to complete
// 2. Waits for servers to become reachable (connection refused during reboot)
// 3. Detaches ISOs for cleanliness (not strictly required but good practice)
//
// Note: We cannot reliably poll STATE partition because the server reboots automatically
// during install, which breaks our insecure TLS connection.
func (p *Provisioner) detachISOsAndReboot(
	ctx context.Context,
	hetznerProv *hetzner.Provider,
	servers []*hcloud.Server,
) error {
	p.logInstallationStart()

	err := p.waitForServersToBeReachable(ctx, servers)
	if err != nil {
		return err
	}

	p.detachISOsFromServers(ctx, hetznerProv, servers)

	return nil
}

// logInstallationStart logs the initial installation progress messages.
func (p *Provisioner) logInstallationStart() {
	_, _ = fmt.Fprintf(
		p.logWriter,
		"  Waiting for installation and automatic reboot to complete...\n",
	)
	_, _ = fmt.Fprintf(
		p.logWriter,
		"  (Talos will install to disk and reboot automatically - this takes 3-5 minutes)\n",
	)
}

// waitForServersToBeReachable waits for all servers to complete installation and reboot.
// It detects completion by waiting for a TCP connection to succeed on the Talos API port.
func (p *Provisioner) waitForServersToBeReachable(
	ctx context.Context,
	servers []*hcloud.Server,
) error {
	return runParallelOnServers(ctx, servers, len(servers), func(server *hcloud.Server) error {
		p.logf("  Waiting for %s to install, reboot, and become reachable...\n", server.Name)

		err := p.waitForServerReachable(ctx, server)
		if err != nil {
			return err
		}

		p.logf("  ✓ %s is reachable after install\n", server.Name)

		return nil
	})
}

// waitForServerReachable polls a single server until a TCP connection succeeds on the Talos API port.
// This waits through the entire install cycle: connection refused during install,
// connection refused during reboot, then success when booted from disk.
func (p *Provisioner) waitForServerReachable(
	ctx context.Context,
	server *hcloud.Server,
) error {
	serverIP := server.PublicNet.IPv4.IP.String()

	err := retry.Constant(clusterReadinessTimeout, retry.WithUnits(longRetryInterval)).
		RetryWithContext(ctx, func(ctx context.Context) error {
			dialer := &net.Dialer{Timeout: retryInterval}

			conn, dialErr := dialer.DialContext(
				ctx,
				"tcp",
				net.JoinHostPort(serverIP, strconv.Itoa(talosAPIPort)),
			)
			if dialErr != nil {
				return retry.ExpectedError(
					fmt.Errorf("waiting for server to become reachable: %w", dialErr),
				)
			}

			_ = conn.Close()

			return nil
		})
	if err != nil {
		return fmt.Errorf(
			"timeout waiting for %s to become reachable after install: %w",
			server.Name,
			err,
		)
	}

	return nil
}

// detachISOsFromServers detaches ISOs from all servers for cleanliness.
// This isn't strictly required (disk has boot priority after install)
// but it's good practice to clean up. Failures are logged but don't block completion.
func (p *Provisioner) detachISOsFromServers(
	ctx context.Context,
	hetznerProv *hetzner.Provider,
	servers []*hcloud.Server,
) {
	group, _ := errgroup.WithContext(ctx)
	group.SetLimit(maxConcurrentHetznerOps)

	for _, server := range servers {
		group.Go(func() error {
			p.logf("  Detaching ISO from %s...\n", server.Name)

			err := hetznerProv.DetachISO(ctx, server)
			if err != nil {
				p.logf("  Warning: Failed to detach ISO from %s: %v\n", server.Name, err)
			} else {
				p.logf("  ✓ ISO detached from %s\n", server.Name)
			}

			return nil // errors logged but don't fail
		})
	}

	_ = group.Wait()
}

// applyConfigToNode applies machine configuration to a single Hetzner node.
func (p *Provisioner) applyConfigToNode(
	ctx context.Context,
	server *hcloud.Server,
	config talosconfig.Provider,
) error {
	serverIP := server.PublicNet.IPv4.IP.String()

	p.logf("  Applying config to %s (%s)...\n", server.Name, serverIP)

	cfgBytes, err := config.Bytes()
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	var lastErr error

	for attempt := 1; attempt <= talosApplyConfigMaxAttempts; attempt++ {
		lastErr = attemptApplyConfig(ctx, serverIP, cfgBytes)
		if lastErr == nil {
			p.logf("  ✓ Config applied to %s\n", server.Name)

			return nil
		}

		if !isRetryableTalosApplyConfigError(lastErr) {
			return fmt.Errorf("failed to apply configuration: %w", lastErr)
		}

		if attempt == talosApplyConfigMaxAttempts {
			break
		}

		delay := netretry.ExponentialDelay(
			attempt,
			talosApplyConfigRetryBaseWait,
			talosApplyConfigRetryMaxWait,
		)

		p.logf(
			"  Config apply attempt %d/%d failed on %s (retrying in %s): %v\n",
			attempt,
			talosApplyConfigMaxAttempts,
			server.Name,
			delay,
			lastErr,
		)

		lastErr = sleepWithContext(ctx, delay)
		if lastErr != nil {
			return fmt.Errorf("retry backoff interrupted: %w", lastErr)
		}
	}

	return fmt.Errorf(
		"failed to apply configuration for %s: %w",
		server.Name,
		errors.Join(errRetriesExhausted, lastErr),
	)
}

// attemptApplyConfig creates a single-use insecure Talos client and attempts to
// apply cfgBytes to the node at serverIP. The client is always closed before
// returning.
func attemptApplyConfig(ctx context.Context, serverIP string, cfgBytes []byte) error {
	insecureClient, err := talosclient.New(ctx,
		talosclient.WithEndpoints(serverIP),
		talosclient.WithTLSConfig(&tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // Required for maintenance mode
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to create Talos client: %w", err)
	}

	defer insecureClient.Close() //nolint:errcheck

	_, err = insecureClient.ApplyConfiguration(ctx, &machineapi.ApplyConfigurationRequest{
		Data: cfgBytes,
	})
	if err != nil {
		return fmt.Errorf("apply configuration: %w", err)
	}

	return nil
}

// sleepWithContext waits for d to elapse, returning ctx.Err() early if the context is cancelled.
func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	select {
	case <-ctx.Done():
		if !timer.Stop() {
			<-timer.C
		}

		return fmt.Errorf("%w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

func isRetryableTalosApplyConfigError(err error) bool {
	if err == nil {
		return false
	}

	if talosclient.StatusCode(err) == grpcUnavailable {
		return true
	}

	errMsg := strings.ToLower(err.Error())

	return strings.Contains(errMsg, "rpc error: code = unavailable") ||
		strings.Contains(errMsg, "authentication handshake failed")
}
