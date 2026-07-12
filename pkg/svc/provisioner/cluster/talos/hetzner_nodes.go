package talosprovisioner

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/siderolabs/go-retry/retry"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/bundle"
	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"
	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v3"
)

// yamlIndent is the indentation width used when re-encoding Talos config
// documents. Talos uses 4-space indentation; matching it keeps re-encoded output
// stylistically consistent with the original.
const yamlIndent = 4

// maxConcurrentHetznerOps caps the number of Hetzner API operations executed in parallel.
// A value of 3 balances throughput and API rate-limit headroom.
const maxConcurrentHetznerOps = 3

// requireHetznerServer returns the authoritative server named by an
// infrastructure-inventory entry, failing closed when the lookup errors or the
// server disappears between the list and get operations.
func requireHetznerServer(
	ctx context.Context,
	hzProvider *hetzner.Provider,
	name string,
) (*hcloud.Server, error) {
	server, err := hzProvider.GetServerByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get server %s: %w", name, err)
	}

	if server == nil {
		return nil, fmt.Errorf("%w: %s", ErrHetznerServerMissingFromInventory, name)
	}

	return server, nil
}

// maxNodeNameLength is the maximum length of a Hetzner node name. The name is
// used as the Hetzner server name, the Talos hostname (set in applyConfigToNode),
// and the Kubernetes node name the Hetzner CCM matches against — all DNS-1123
// labels capped at 63 characters. ValidateClusterName already caps the cluster
// name at 63, but the "-<role>-<index>" suffix can push the composed name past
// the limit, so the full node name must be validated too.
const maxNodeNameLength = 63

// hetznerNodeName formats and validates the name for a Hetzner node. It is the
// single source of node-name construction for the create, scale-up, and
// rolling-recreate paths, so the 63-character DNS-1123 label limit is enforced
// consistently before any (billable) server is provisioned. The formatted name
// is always returned, even when it is rejected, so callers can use it in
// diagnostics and failure records.
func hetznerNodeName(clusterName, role string, index int) (string, error) {
	name := fmt.Sprintf("%s-%s-%d", clusterName, role, index)
	if len(name) > maxNodeNameLength {
		return name, fmt.Errorf(
			"%w: %q is %d characters (max %d); shorten the cluster name",
			ErrNodeNameTooLong, name, len(name), maxNodeNameLength,
		)
	}

	return name, nil
}

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

	retryOpts := p.hetznerServerRetryOpts()
	enableIPv4, enableIPv6 := p.hetznerPublicNetForRole(opts.Role)

	results := make([]hetznerNodeCreationResult, opts.Count)

	group, _ := errgroup.WithContext(ctx)
	group.SetLimit(maxConcurrentHetznerOps)

	for nodeIndex := range opts.Count {
		group.Go(func() error {
			nodeName, nameErr := hetznerNodeName(opts.ClusterName, opts.Role, nodeIndex+1)
			if nameErr != nil {
				// Validation failure: record it and skip provisioning (no billable
				// server created). The goroutine still returns nil; the error is
				// surfaced by collectCreatedHetznerServers reading results.
				results[nodeIndex] = hetznerNodeCreationResult{name: nodeName, err: nameErr}
			} else {
				server, err := hzProvider.CreateServerWithRetry(ctx, hetzner.CreateServerOpts{
					Name:             nodeName,
					ServerType:       opts.ServerType,
					ISOID:            opts.ISOID,
					ImageID:          opts.ImageID,
					Location:         opts.Location,
					Labels:           hetzner.NodeLabels(opts.ClusterName, opts.Role, nodeIndex+1),
					NetworkID:        infra.NetworkID,
					PlacementGroupID: infra.PlacementGroupID,
					SSHKeyID:         infra.SSHKeyID,
					FirewallIDs:      []int64{infra.FirewallID},
					EnableIPv4:       enableIPv4,
					EnableIPv6:       enableIPv6,
				}, retryOpts)

				results[nodeIndex] = hetznerNodeCreationResult{
					name:   nodeName,
					server: server,
					err:    err,
				}
			}

			return nil // errors collected in results
		})
	}

	waitErr := group.Wait()
	if waitErr != nil {
		return nil, fmt.Errorf("unexpected error during Hetzner node creation: %w", waitErr)
	}

	return p.collectCreatedHetznerServers(results, opts.Role)
}

// hetznerServerRetryOpts builds the server-creation retry options from the
// provisioner's Hetzner configuration, applying location/placement fallbacks
// when configured.
func (p *Provisioner) hetznerServerRetryOpts() hetzner.ServerRetryOpts {
	retryOpts := hetzner.ServerRetryOpts{LogWriter: p.syncLogWriter()}

	if p.hetznerOpts != nil {
		retryOpts.FallbackLocations = p.hetznerOpts.FallbackLocations
		retryOpts.AllowPlacementFallback = p.hetznerOpts.PlacementGroupFallbackToNone
	}

	return retryOpts
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

		// hetznerNodeTalosAddress only fails when the server has neither a public
		// IPv4 nor a private-network IP (or the address is not yet populated), so the
		// placeholder must not claim a specific cause.
		addr, addrErr := hetznerNodeTalosAddress(res.server)
		if addrErr != nil {
			addr = "address unavailable"
		}

		_, _ = fmt.Fprintf(
			p.logWriter,
			"  ✓ %s node %s created (IP: %s)\n",
			role,
			res.name,
			addr,
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
		serverIP, addrErr := hetznerNodeTalosAddress(server)
		if addrErr != nil {
			return addrErr
		}

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
			return diagnoseUnreachableNode(
				server,
				fmt.Errorf("timeout waiting for Talos API on %s: %w", server.Name, err),
			)
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

	// Warn once if the user supplied their own HostnameConfig (e.g. auto: off):
	// KSail overrides it with the server name for CCM compatibility (see
	// warnIfOverridingUserHostname), and the override should be visible, not silent.
	cpBytes, bytesErr := cpConfig.Bytes()
	if bytesErr == nil {
		p.warnIfOverridingUserHostname(cpBytes)
	}

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
	serverIP, addrErr := hetznerNodeTalosAddress(server)
	if addrErr != nil {
		return addrErr
	}

	err := dialTCPUntilReachable(ctx, serverIP, clusterReadinessTimeout, longRetryInterval)
	if err != nil {
		return diagnoseUnreachableNode(server, fmt.Errorf(
			"timeout waiting for %s to become reachable after install: %w",
			server.Name,
			err,
		))
	}

	return nil
}

// dialTCPUntilReachable polls ip:talosAPIPort until a TCP connection succeeds or
// the context is done, retrying every interval up to timeout. A successful dial
// means the Talos apid service is accepting connections on the node. This is
// shared by the Hetzner install/reboot wait (waitForServerReachable) and the
// Docker scale-up wait (waitForNewDockerNodesReachable); each caller wraps the
// returned error with its own server/node context.
func dialTCPUntilReachable(
	ctx context.Context,
	nodeIP string,
	timeout, interval time.Duration,
) error {
	err := retry.Constant(timeout, retry.WithUnits(interval)).
		RetryWithContext(ctx, func(ctx context.Context) error {
			dialer := &net.Dialer{Timeout: retryInterval}

			conn, dialErr := dialer.DialContext(
				ctx,
				"tcp",
				net.JoinHostPort(nodeIP, strconv.Itoa(talosAPIPort)),
			)
			if dialErr != nil {
				return retry.ExpectedError(
					fmt.Errorf("waiting for Talos API to become reachable: %w", dialErr),
				)
			}

			_ = conn.Close()

			return nil
		})
	if err != nil {
		return fmt.Errorf("dialing Talos API at %s: %w", nodeIP, err)
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
	serverIP, addrErr := hetznerNodeTalosAddress(server)
	if addrErr != nil {
		return addrErr
	}

	p.logf("  Applying config to %s (%s)...\n", server.Name, serverIP)

	cfgBytes, err := marshalConfigWithHostname(config, server.Name)
	if err != nil {
		return err
	}

	err = p.retryTransientTalosAPICall(ctx, server.Name, "Config apply",
		func(ctx context.Context) error {
			return attemptApplyConfig(ctx, serverIP, cfgBytes)
		})
	if err != nil {
		return fmt.Errorf("failed to apply configuration for %s: %w", server.Name, err)
	}

	p.logf("  ✓ Config applied to %s\n", server.Name)

	return nil
}

// marshalConfigWithHostname marshals the role config to bytes and overlays the
// per-node hostname so it matches the Hetzner server name. The Hetzner CCM
// (cloud-provider: external) matches Kubernetes Nodes to servers by name, so a
// node that boots with the generic Talos hostname (talos-xxxxx) never gets
// initialized by the CCM and never joins the cluster. KSail boots scaled-up
// nodes from the public Talos ISO (the metal platform), which has no cloud
// metadata to derive the hostname from, so it must be set explicitly. Patching
// the marshaled bytes (rather than the shared talosconfig.Provider) keeps this
// safe for the parallel per-node apply, which reuses one config across goroutines.
func marshalConfigWithHostname(config talosconfig.Provider, serverName string) ([]byte, error) {
	cfgBytes, err := config.Bytes()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	cfgBytes, err = patchTalosHostname(cfgBytes, serverName)
	if err != nil {
		return nil, fmt.Errorf("failed to set hostname for %s: %w", serverName, err)
	}

	return cfgBytes, nil
}

// hostnameConfigKind is the document kind of the standalone Talos HostnameConfig
// network document. Talos emits one (defaulting to `auto: stable`) in generated
// machine configs, and it conflicts with a static machine.network.hostname.
const hostnameConfigKind = "HostnameConfig"

// patchTalosHostname overlays machine.network.hostname onto marshaled Talos
// machine-config bytes via a strategic-merge patch, returning the patched bytes.
// It operates on bytes rather than a shared talosconfig.Provider so it is safe to
// call concurrently for each node during a parallel config apply (each call gets
// its own copy). The hostname is the Hetzner server name, which is DNS-1123
// compliant by construction (<cluster>-<role>-<index>, with cluster name
// pre-validated), so it is a valid Talos hostname.
//
// Talos generated machine configs carry a standalone HostnameConfig network
// document (defaulting to `auto: stable`). Once machine.network.hostname is set,
// that document conflicts with the v1alpha1 hostname ("static hostname is already
// set in v1alpha1 config", #4969), so it is stripped here. This leaves the
// hostname in a single representation, making the function idempotent: applying it
// to a config that already has machine.network.hostname set (the scale-up and
// rolling-recreate base config) still yields a config the node accepts.
func patchTalosHostname(cfgBytes []byte, hostname string) ([]byte, error) {
	patch, err := configpatcher.LoadPatch(
		fmt.Appendf(nil, "machine:\n  network:\n    hostname: %s\n", hostname),
	)
	if err != nil {
		return nil, fmt.Errorf("load hostname patch: %w", err)
	}

	out, err := configpatcher.Apply(configpatcher.WithBytes(cfgBytes), []configpatcher.Patch{patch})
	if err != nil {
		return nil, fmt.Errorf("apply hostname patch: %w", err)
	}

	patched, err := out.Bytes()
	if err != nil {
		return nil, fmt.Errorf("encode patched config: %w", err)
	}

	return stripHostnameConfigDocuments(patched)
}

// stripHostnameConfigDocuments removes any standalone HostnameConfig document from
// a (possibly multi-document) Talos config YAML, re-encoding the remaining
// documents. Removing it resolves the conflict with a static
// machine.network.hostname (see patchTalosHostname). The v1alpha1 MachineConfig
// document (and any other documents) are preserved semantically: decoding to a
// generic map and re-encoding can reorder keys and drop comments/formatting, but
// the configuration values the node consumes are unchanged.
func stripHostnameConfigDocuments(cfgBytes []byte) ([]byte, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(cfgBytes))

	var buf bytes.Buffer

	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(yamlIndent)

	for {
		var doc map[string]any

		decodeErr := decoder.Decode(&doc)
		if errors.Is(decodeErr, io.EOF) {
			break
		}

		if decodeErr != nil {
			return nil, fmt.Errorf("decode config document: %w", decodeErr)
		}

		if doc == nil {
			continue
		}

		kind, _ := doc["kind"].(string)
		if kind == hostnameConfigKind {
			continue
		}

		encodeErr := encoder.Encode(doc)
		if encodeErr != nil {
			return nil, fmt.Errorf("re-encode config document: %w", encodeErr)
		}
	}

	closeErr := encoder.Close()
	if closeErr != nil {
		return nil, fmt.Errorf("flush re-encoded config: %w", closeErr)
	}

	return buf.Bytes(), nil
}

// userHostnameConfigSummary inspects a (possibly multi-document) Talos config and
// returns a short description of a USER-authored HostnameConfig document that
// KSail's per-node static hostname overrides (and stripHostnameConfigDocuments
// removes) on Hetzner — or "" when the only HostnameConfig present is the Talos
// SDK default (`auto: stable`). The SDK emits `auto: stable` by default on Talos
// 1.13+; that is exactly the setting that renames nodes to talos-xxxxx and breaks
// the Hetzner CCM, so stripping it is silent. Anything else (`auto: off` or a
// static `hostname:`) is a deliberate user hostname strategy, so KSail warns
// before overriding it rather than discarding it silently.
//
// Best-effort and never errors: an unparseable document simply yields "".
func userHostnameConfigSummary(cfgBytes []byte) string {
	decoder := yaml.NewDecoder(bytes.NewReader(cfgBytes))

	for {
		var doc map[string]any

		decodeErr := decoder.Decode(&doc)
		if decodeErr != nil {
			return "" // EOF or malformed: nothing actionable to report
		}

		if kind, _ := doc["kind"].(string); kind != hostnameConfigKind {
			continue
		}

		if hostname, _ := doc["hostname"].(string); hostname != "" {
			return "hostname: " + hostname
		}

		// HostnameConfig.auto accepts only "stable" (the SDK default) and "off".
		auto, _ := doc["auto"].(string)
		if auto != "" && !strings.EqualFold(auto, "stable") {
			return "auto: " + auto
		}

		return "" // SDK default (auto: stable / unset) — overridden silently
	}
}

// warnIfOverridingUserHostname emits a one-line warning when cfgBytes carries a
// user-authored HostnameConfig that KSail will override with the per-node static
// hostname (the Hetzner server name) and strip from the applied config. On Hetzner
// the node hostname is not a free choice: it must equal the server name or the
// Hetzner CCM never initializes the Node (#4962), so KSail owns it — but the
// override is now visible instead of silent. No-op when only the SDK default
// HostnameConfig is present.
func (p *Provisioner) warnIfOverridingUserHostname(cfgBytes []byte) {
	summary := userHostnameConfigSummary(cfgBytes)
	if summary == "" {
		return
	}

	p.logf(
		"  ⚠ Overriding user HostnameConfig (%s) with the Hetzner server name; "+
			"the Hetzner CCM matches Nodes to servers by name, so KSail manages the "+
			"node hostname and drops the HostnameConfig document from the applied config.\n",
		summary,
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
