package hetzner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provider"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// Default timeouts for Hetzner operations.
const (
	// DefaultActionTimeout is the timeout for waiting on Hetzner actions.
	DefaultActionTimeout = 5 * time.Minute
	// DefaultOperationTimeout is the timeout for individual operations.
	DefaultOperationTimeout = 2 * time.Minute
	// DefaultPollingInterval is the interval between status checks.
	DefaultPollingInterval = 2 * time.Second
	// DefaultDeleteRetryDelay is the delay between delete retry attempts.
	DefaultDeleteRetryDelay = 2 * time.Second
	// DefaultPreDeleteDelay is the delay before deleting infrastructure resources.
	DefaultPreDeleteDelay = 5 * time.Second
	// MaxDeleteRetries is the maximum number of retries for resource deletion.
	MaxDeleteRetries = 5
	// IPv4CIDRBits is the number of bits in an IPv4 CIDR mask for 0.0.0.0/0.
	IPv4CIDRBits = 32
	// IPv6CIDRBits is the number of bits in an IPv6 CIDR mask for ::/0.
	IPv6CIDRBits = 128
	// DefaultMaxServerCreateRetries is the number of retry attempts for server creation.
	DefaultMaxServerCreateRetries = 3
	// DefaultRetryBaseDelay is the base delay for exponential backoff.
	DefaultRetryBaseDelay = 2 * time.Second
	// DefaultRetryMaxDelay is the maximum delay between retry attempts.
	DefaultRetryMaxDelay = 10 * time.Second
)

// ErrHetznerActionFailed indicates that a Hetzner action failed.
var ErrHetznerActionFailed = errors.New("hetzner action failed")

// Provider implements provider.Provider for Hetzner Cloud servers.
type Provider struct {
	client *hcloud.Client
}

// NewProvider creates a new Hetzner Cloud provider with the given client.
func NewProvider(client *hcloud.Client) *Provider {
	return &Provider{
		client: client,
	}
}

// NewProviderFromToken creates a new Hetzner Cloud provider using an API token.
func NewProviderFromToken(token string) *Provider {
	client := hcloud.NewClient(hcloud.WithToken(token))

	return &Provider{
		client: client,
	}
}

// StartNodes starts all servers for the given cluster.
func (p *Provider) StartNodes(ctx context.Context, clusterName string) error {
	return p.forEachServer(ctx, clusterName, func(server *hcloud.Server) (*hcloud.Action, error) {
		// Skip if already running
		if server.Status == hcloud.ServerStatusRunning {
			return nil, nil
		}

		action, _, err := p.client.Server.Poweron(ctx, server)
		if err != nil {
			return nil, fmt.Errorf("failed to power on server %s: %w", server.Name, err)
		}

		return action, nil
	})
}

// StopNodes stops all servers for the given cluster.
// Uses graceful shutdown (ACPI signal) and waits for servers to be off.
func (p *Provider) StopNodes(ctx context.Context, clusterName string) error {
	err := p.forEachServer(ctx, clusterName, func(server *hcloud.Server) (*hcloud.Action, error) {
		// Skip if already off
		if server.Status == hcloud.ServerStatusOff {
			return nil, provider.ErrSkipAction
		}

		action, _, shutdownErr := p.client.Server.Shutdown(ctx, server)
		if shutdownErr != nil {
			return nil, fmt.Errorf("failed to shutdown server %s: %w", server.Name, shutdownErr)
		}

		return action, nil
	})
	if err != nil {
		return err
	}

	// Wait for all servers to be off
	return p.waitForServersStatus(ctx, clusterName, hcloud.ServerStatusOff)
}

// waitForServersStatus polls until all servers in the cluster reach the desired status.
//
//nolint:funcorder // Grouped with StopNodes for logical code organization
func (p *Provider) waitForServersStatus(
	ctx context.Context,
	clusterName string,
	desiredStatus hcloud.ServerStatus,
) error {
	ctx, cancel := context.WithTimeout(ctx, DefaultActionTimeout)
	defer cancel()

	ticker := time.NewTicker(DefaultPollingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf(
				"timeout waiting for servers to reach status %s: %w",
				desiredStatus,
				ctx.Err(),
			)
		case <-ticker.C:
			allReady := true

			nodes, err := p.ListNodes(ctx, clusterName)
			if err != nil {
				return fmt.Errorf("failed to list nodes: %w", err)
			}

			for _, node := range nodes {
				server, _, err := p.client.Server.GetByName(ctx, node.Name)
				if err != nil {
					return fmt.Errorf("failed to get server %s: %w", node.Name, err)
				}

				if server != nil && server.Status != desiredStatus {
					allReady = false

					break
				}
			}

			if allReady {
				return nil
			}
		}
	}
}

// ListNodes returns all nodes for the given cluster based on labels.
func (p *Provider) ListNodes(ctx context.Context, clusterName string) ([]provider.NodeInfo, error) {
	if p.client == nil {
		return nil, provider.ErrProviderUnavailable
	}

	// Use label selector to filter servers
	labelSelector := fmt.Sprintf("%s=true,%s=%s", LabelOwned, LabelClusterName, clusterName)

	servers, err := p.client.Server.AllWithOpts(ctx, hcloud.ServerListOpts{
		ListOpts: hcloud.ListOpts{
			LabelSelector: labelSelector,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list servers: %w", err)
	}

	nodes := make([]provider.NodeInfo, 0, len(servers))

	for _, server := range servers {
		nodeType := server.Labels[LabelNodeType]

		nodes = append(nodes, provider.NodeInfo{
			Name:        server.Name,
			ClusterName: clusterName,
			Role:        nodeType,
			State:       string(server.Status),
		})
	}

	return nodes, nil
}

// ListAllClusters returns the names of all clusters managed by this provider.
func (p *Provider) ListAllClusters(ctx context.Context) ([]string, error) {
	if p.client == nil {
		return nil, provider.ErrProviderUnavailable
	}

	// Use label selector to filter KSail-owned servers
	labelSelector := LabelOwned + "=true"

	servers, err := p.client.Server.AllWithOpts(ctx, hcloud.ServerListOpts{
		ListOpts: hcloud.ListOpts{
			LabelSelector: labelSelector,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list servers: %w", err)
	}

	return k8s.UniqueLabelValues(
		servers,
		LabelClusterName,
		func(s *hcloud.Server) map[string]string {
			return s.Labels
		},
	), nil
}

// NodesExist returns true if nodes exist for the given cluster name.
func (p *Provider) NodesExist(ctx context.Context, clusterName string) (bool, error) {
	exists, err := provider.CheckNodesExist(ctx, p, clusterName)
	if err != nil {
		return false, fmt.Errorf("hetzner nodes exist: %w", err)
	}

	return exists, nil
}

// DeleteNodes removes all servers for the given cluster.
func (p *Provider) DeleteNodes(ctx context.Context, clusterName string) error {
	if p.client == nil {
		return provider.ErrProviderUnavailable
	}

	// Delete all servers - use forEachServerOptional since having no nodes is OK for delete
	deleteErr := p.forEachServerOptional(
		ctx,
		clusterName,
		func(server *hcloud.Server) (*hcloud.Action, error) {
			_, _, err := p.client.Server.DeleteWithResult(ctx, server)
			if err != nil {
				return nil, fmt.Errorf("failed to delete server %s: %w", server.Name, err)
			}
			// Delete doesn't return an action we need to wait for
			return nil, provider.ErrSkipAction
		},
	)
	if deleteErr != nil {
		return deleteErr
	}

	// Clean up infrastructure resources
	err := p.deleteInfrastructure(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to delete infrastructure: %w", err)
	}

	return nil
}

// DeleteServer deletes a single Hetzner Cloud server.
// Unlike DeleteNodes, this targets a specific server without removing infrastructure.
func (p *Provider) DeleteServer(ctx context.Context, server *hcloud.Server) error {
	if p.client == nil {
		return provider.ErrProviderUnavailable
	}

	_, _, err := p.client.Server.DeleteWithResult(ctx, server)
	if err != nil {
		return fmt.Errorf("failed to delete server %s: %w", server.Name, err)
	}

	return nil
}

// CreateServer creates a new Hetzner server with the specified configuration.
func (p *Provider) CreateServer(
	ctx context.Context,
	opts CreateServerOpts,
) (*hcloud.Server, error) {
	if p.client == nil {
		return nil, provider.ErrProviderUnavailable
	}

	createOpts := p.buildServerCreateOpts(opts)

	result, _, err := p.client.Server.Create(ctx, createOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create server %s: %w", opts.Name, err)
	}

	// Wait for server creation to complete
	err = p.waitForAction(ctx, result.Action)
	if err != nil {
		return nil, fmt.Errorf("failed waiting for server %s creation: %w", opts.Name, err)
	}

	// If using ISO, attach it and reboot the server to boot from ISO
	if opts.ISOID > 0 {
		err = p.attachISOAndReboot(ctx, result.Server, opts.ISOID)
		if err != nil {
			return nil, fmt.Errorf("failed to attach ISO to server %s: %w", opts.Name, err)
		}
	}

	return result.Server, nil
}

// ServerRetryOpts configures retry and fallback behavior for server creation.
type ServerRetryOpts struct {
	// FallbackLocations is a list of alternative locations to try if the primary fails.
	FallbackLocations []string
	// AllowPlacementFallback allows disabling placement group if placement fails.
	AllowPlacementFallback bool
	// LogWriter receives retry progress messages. If nil, no logging is performed.
	LogWriter io.Writer
}

// CreateServerWithRetry creates a server with retry and location fallback support.
// It handles transient Hetzner API errors with exponential backoff and can fallback
// to alternative locations when the primary location has resource constraints.
func (p *Provider) CreateServerWithRetry(
	ctx context.Context,
	opts CreateServerOpts,
	retryOpts ServerRetryOpts,
) (*hcloud.Server, error) {
	if p.client == nil {
		return nil, provider.ErrProviderUnavailable
	}

	// Build list of locations to try: primary + fallbacks
	locations := make([]string, 0, 1+len(retryOpts.FallbackLocations))
	locations = append(locations, opts.Location)
	locations = append(locations, retryOpts.FallbackLocations...)

	var lastErr error

	originalPlacementGroupID := opts.PlacementGroupID

	for locationIdx, location := range locations {
		server, err := p.attemptServerCreationInLocation(
			ctx,
			opts,
			retryOpts,
			location,
			locationIdx,
			originalPlacementGroupID,
		)
		if err == nil {
			return server, nil
		}

		lastErr = err

		// Log fallback if more locations available
		if locationIdx < len(locations)-1 {
			p.logLocationFallback(retryOpts.LogWriter, location, locations[locationIdx+1])
		}
	}

	// All locations exhausted
	return nil, fmt.Errorf("%w: %s (last error: %w)", ErrAllLocationsFailed, opts.Name, lastErr)
}

// attemptServerCreationInLocation tries to create a server in a specific location with retries.
//
//nolint:funcorder // Helper grouped with CreateServerWithRetry
func (p *Provider) attemptServerCreationInLocation(
	ctx context.Context,
	opts CreateServerOpts,
	retryOpts ServerRetryOpts,
	location string,
	locationIdx int,
	originalPlacementGroupID int64,
) (*hcloud.Server, error) {
	currentOpts := opts
	currentOpts.Location = location
	currentOpts.PlacementGroupID = originalPlacementGroupID
	placementDisabledForLocation := false

	for attempt := 1; attempt <= DefaultMaxServerCreateRetries; attempt++ {
		server, err := p.CreateServer(ctx, currentOpts)
		if err == nil {
			p.logSuccessfulFallback(
				retryOpts.LogWriter,
				opts.Name,
				location,
				locationIdx,
				placementDisabledForLocation,
				currentOpts.PlacementGroupID > 0,
			)

			return server, nil
		}

		// Check for permanent errors
		if IsResourceLimitError(err) {
			return nil, fmt.Errorf("permanent error creating server %s: %w", opts.Name, err)
		}

		// Handle placement errors with fallback
		if shouldDisablePlacement(err, retryOpts, currentOpts.PlacementGroupID) {
			p.logPlacementFallback(retryOpts.LogWriter, opts.Name, location)

			currentOpts.PlacementGroupID = 0
			placementDisabledForLocation = true

			continue
		}

		// Check if we should retry this error
		if !shouldRetryError(err) {
			// Non-retryable error - try next location if available
			return nil, err
		}

		// Wait before next retry
		if attempt < DefaultMaxServerCreateRetries {
			waitErr := p.waitForRetryDelay(
				ctx,
				retryOpts.LogWriter,
				attempt,
				opts.Name,
				location,
				err,
			)
			if waitErr != nil {
				return nil, waitErr
			}
		}
	}

	// All retries exhausted for this location
	return nil, fmt.Errorf("%w in location %s", ErrAllRetriesExhausted, location)
}

// shouldDisablePlacement checks if placement group should be disabled after an error.
func shouldDisablePlacement(
	err error,
	retryOpts ServerRetryOpts,
	placementGroupID int64,
) bool {
	return IsPlacementError(err) &&
		retryOpts.AllowPlacementFallback &&
		placementGroupID > 0
}

// shouldRetryError determines if an error should trigger a retry.
func shouldRetryError(err error) bool {
	return IsRetryableHetznerError(err) || IsPlacementError(err)
}

// waitForRetryDelay waits for the retry delay with context cancellation support.
//
//nolint:funcorder // Helper grouped with CreateServerWithRetry
func (p *Provider) waitForRetryDelay(
	ctx context.Context,
	logWriter io.Writer,
	attempt int,
	serverName string,
	location string,
	err error,
) error {
	delay := p.calculateRetryDelay(attempt)
	p.logRetryAttempt(logWriter, attempt, serverName, location, err, delay)

	select {
	case <-ctx.Done():
		return fmt.Errorf("context cancelled during retry: %w", ctx.Err())
	case <-time.After(delay):
		return nil
	}
}

// logSuccessfulFallback logs when a server is created successfully after fallback.
//
//nolint:funcorder // Helper grouped with CreateServerWithRetry
func (p *Provider) logSuccessfulFallback(
	logWriter io.Writer,
	serverName string,
	location string,
	locationIdx int,
	placementDisabled bool,
	hasPlacementGroup bool,
) {
	if locationIdx > 0 || placementDisabled {
		p.logRetryf(
			logWriter,
			"  ✓ Server %s created successfully after fallback (location: %s, placement group: %v)\n",
			serverName,
			location,
			hasPlacementGroup,
		)
	}
}

// logPlacementFallback logs when placement group is disabled for retry.
//
//nolint:funcorder // Helper grouped with CreateServerWithRetry
func (p *Provider) logPlacementFallback(logWriter io.Writer, serverName string, location string) {
	p.logRetryf(
		logWriter,
		"  ⚠ Placement failed for %s in %s, retrying without placement group...\n",
		serverName,
		location,
	)
}

// logRetryAttempt logs information about a retry attempt.
//
//nolint:funcorder // Helper grouped with CreateServerWithRetry
func (p *Provider) logRetryAttempt(
	logWriter io.Writer,
	attempt int,
	serverName string,
	location string,
	err error,
	delay time.Duration,
) {
	p.logRetryf(
		logWriter,
		"  ⚠ Attempt %d/%d failed for %s in %s: %v. Retrying in %v...\n",
		attempt,
		DefaultMaxServerCreateRetries,
		serverName,
		location,
		err,
		delay,
	)
}

// logLocationFallback logs when moving to a fallback location.
//
//nolint:funcorder // Helper grouped with CreateServerWithRetry
func (p *Provider) logLocationFallback(
	logWriter io.Writer,
	currentLocation string,
	nextLocation string,
) {
	p.logRetryf(
		logWriter,
		"  ⚠ All attempts failed in %s, trying fallback location %s...\n",
		currentLocation,
		nextLocation,
	)
}

// logRetryf writes a formatted message to the log writer if it's not nil.
//
//nolint:funcorder // Helper grouped with CreateServerWithRetry
func (p *Provider) logRetryf(w io.Writer, format string, args ...any) {
	if w != nil {
		_, _ = fmt.Fprintf(w, format, args...)
	}
}

// calculateRetryDelay returns the delay for the given retry attempt.
// Uses exponential backoff: 2s, 4s, 8s (capped at 10s).
//
//nolint:funcorder // Helper grouped with CreateServerWithRetry
func (p *Provider) calculateRetryDelay(attempt int) time.Duration {
	delay := min(DefaultRetryBaseDelay*time.Duration(1<<(attempt-1)), DefaultRetryMaxDelay)

	return delay
}

// attachISOAndReboot attaches an ISO to a server and reboots it to boot from the ISO.
//
//nolint:funcorder // Grouped with CreateServer for logical code organization
func (p *Provider) attachISOAndReboot(
	ctx context.Context,
	server *hcloud.Server,
	isoID int64,
) error {
	// Attach the ISO
	action, _, err := p.client.Server.AttachISO(ctx, server, &hcloud.ISO{ID: isoID})
	if err != nil {
		return fmt.Errorf("failed to attach ISO: %w", err)
	}

	err = p.waitForAction(ctx, action)
	if err != nil {
		return fmt.Errorf("failed waiting for ISO attachment: %w", err)
	}

	// Reset (hard reboot) the server to boot from the ISO
	return p.ResetServer(ctx, server)
}

// DetachISO detaches any attached ISO from a server.
// This is necessary after applying Talos config in maintenance mode so the server
// boots from disk (with the installed Talos) instead of the ISO.
func (p *Provider) DetachISO(ctx context.Context, server *hcloud.Server) error {
	if p.client == nil {
		return provider.ErrProviderUnavailable
	}

	// Detach the ISO
	action, _, err := p.client.Server.DetachISO(ctx, server)
	if err != nil {
		return fmt.Errorf("failed to detach ISO: %w", err)
	}

	err = p.waitForAction(ctx, action)
	if err != nil {
		return fmt.Errorf("failed waiting for ISO detachment: %w", err)
	}

	return nil
}

// ResetServer performs a hard reset (reboot) on a server.
// This is used after detaching the ISO to boot from disk.
func (p *Provider) ResetServer(ctx context.Context, server *hcloud.Server) error {
	if p.client == nil {
		return provider.ErrProviderUnavailable
	}

	action, _, err := p.client.Server.Reset(ctx, server)
	if err != nil {
		return fmt.Errorf("failed to reset server: %w", err)
	}

	err = p.waitForAction(ctx, action)
	if err != nil {
		return fmt.Errorf("failed waiting for server reset: %w", err)
	}

	return nil
}

// CreateServerOpts contains options for creating a Hetzner server.
type CreateServerOpts struct {
	Name             string
	ServerType       string
	ImageID          int64 // Image ID (for snapshots) - mutually exclusive with ISOID
	ISOID            int64 // ISO ID (for Talos public ISOs) - mutually exclusive with ImageID
	Location         string
	Labels           map[string]string
	UserData         string
	NetworkID        int64
	PlacementGroupID int64
	SSHKeyID         int64
	FirewallIDs      []int64
}

// GetSSHKey retrieves an SSH key by name.
func (p *Provider) GetSSHKey(ctx context.Context, name string) (*hcloud.SSHKey, error) {
	if p.client == nil {
		return nil, provider.ErrProviderUnavailable
	}

	sshKey, _, err := p.client.SSHKey.GetByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get SSH key %s: %w", name, err)
	}

	return sshKey, nil
}

// GetServerByName retrieves a server by name.
func (p *Provider) GetServerByName(ctx context.Context, name string) (*hcloud.Server, error) {
	if p.client == nil {
		return nil, provider.ErrProviderUnavailable
	}

	server, _, err := p.client.Server.GetByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get server %s: %w", name, err)
	}

	return server, nil
}

// IsAvailable returns true if the provider is ready for use.
func (p *Provider) IsAvailable() bool {
	return p.client != nil
}

// forEachServer executes an action on each server in the cluster.
// Returns ErrNoNodes if no nodes exist.
func (p *Provider) forEachServer(
	ctx context.Context,
	clusterName string,
	action func(*hcloud.Server) (*hcloud.Action, error),
) error {
	return p.forEachServerWithOptions(ctx, clusterName, action, true)
}

// forEachServerOptional executes an action on each server in the cluster.
// Does not error if no nodes exist (useful for delete operations).
func (p *Provider) forEachServerOptional(
	ctx context.Context,
	clusterName string,
	action func(*hcloud.Server) (*hcloud.Action, error),
) error {
	return p.forEachServerWithOptions(ctx, clusterName, action, false)
}

// forEachServerWithOptions is the core implementation for iterating servers.
func (p *Provider) forEachServerWithOptions(
	ctx context.Context,
	clusterName string,
	action func(*hcloud.Server) (*hcloud.Action, error),
	requireNodes bool,
) error {
	nodes, err := provider.EnsureAvailableAndListNodes(ctx, p, clusterName)
	if err != nil {
		return fmt.Errorf("failed to prepare server operation: %w", err)
	}

	if len(nodes) == 0 {
		if requireNodes {
			return provider.ErrNoNodes
		}

		return nil
	}

	for _, node := range nodes {
		err := p.executeServerAction(ctx, node.Name, action)
		if err != nil {
			return err
		}
	}

	return nil
}

// executeServerAction executes an action on a single server by name.
func (p *Provider) executeServerAction(
	ctx context.Context,
	nodeName string,
	action func(*hcloud.Server) (*hcloud.Action, error),
) error {
	server, _, serverErr := p.client.Server.GetByName(ctx, nodeName)
	if serverErr != nil {
		return fmt.Errorf("failed to get server %s: %w", nodeName, serverErr)
	}

	if server == nil {
		return nil
	}

	hcloudAction, actionErr := action(server)
	if actionErr != nil {
		// ErrSkipAction signals no action is needed
		if errors.Is(actionErr, provider.ErrSkipAction) {
			return nil
		}

		return actionErr
	}

	if hcloudAction != nil {
		return p.waitForAction(ctx, hcloudAction)
	}

	return nil
}

// waitForAction waits for a Hetzner action to complete.
func (p *Provider) waitForAction(ctx context.Context, action *hcloud.Action) error {
	if action == nil {
		return nil
	}

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(ctx, DefaultActionTimeout)
	defer cancel()

	// Poll for action completion
	//nolint:staticcheck // WatchProgress is deprecated but still the simplest option
	_, errChan := p.client.Action.WatchProgress(ctx, action)

	err := <-errChan
	if err != nil {
		return fmt.Errorf("%w: %w", ErrHetznerActionFailed, err)
	}

	return nil
}

// buildServerCreateOpts builds the hcloud.ServerCreateOpts from CreateServerOpts.
func (p *Provider) buildServerCreateOpts(opts CreateServerOpts) hcloud.ServerCreateOpts {
	createOpts := hcloud.ServerCreateOpts{
		Name:   opts.Name,
		Labels: opts.Labels,
		ServerType: &hcloud.ServerType{
			Name: opts.ServerType,
		},
		Location: &hcloud.Location{
			Name: opts.Location,
		},
		StartAfterCreate: new(true),
	}

	// Use either Image or ISO - ISOs are used for Talos public ISOs
	if opts.ISOID > 0 {
		// When using ISO, we need a placeholder image for the server disk
		// The ISO will be mounted and booted from
		createOpts.Image = &hcloud.Image{
			Name: "debian-13",
		}
		// Note: ISO attachment happens after server creation via AttachISO action
	} else if opts.ImageID > 0 {
		createOpts.Image = &hcloud.Image{
			ID: opts.ImageID,
		}
	}

	if opts.UserData != "" {
		createOpts.UserData = opts.UserData
	}

	if opts.NetworkID > 0 {
		createOpts.Networks = []*hcloud.Network{
			{ID: opts.NetworkID},
		}
	}

	if opts.PlacementGroupID > 0 {
		createOpts.PlacementGroup = &hcloud.PlacementGroup{
			ID: opts.PlacementGroupID,
		}
	}

	if opts.SSHKeyID > 0 {
		createOpts.SSHKeys = []*hcloud.SSHKey{
			{ID: opts.SSHKeyID},
		}
	}

	if len(opts.FirewallIDs) > 0 {
		firewalls := make([]*hcloud.ServerCreateFirewall, len(opts.FirewallIDs))
		for i, id := range opts.FirewallIDs {
			firewalls[i] = &hcloud.ServerCreateFirewall{
				Firewall: hcloud.Firewall{ID: id},
			}
		}

		createOpts.Firewalls = firewalls
	}

	return createOpts
}
