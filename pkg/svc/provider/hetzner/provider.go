package hetzner

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provider"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// Default timeouts for Hetzner operations.
const (
	// DefaultActionTimeout is the timeout for waiting on Hetzner actions.
	DefaultActionTimeout = 5 * time.Minute
	// DefaultOperationTimeout is the timeout for individual operations.
	DefaultOperationTimeout = 2 * time.Minute
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
func (p *Provider) StopNodes(ctx context.Context, clusterName string) error {
	return p.forEachServer(ctx, clusterName, func(server *hcloud.Server) (*hcloud.Action, error) {
		// Skip if already off
		if server.Status == hcloud.ServerStatusOff {
			return nil, nil
		}

		action, _, err := p.client.Server.Shutdown(ctx, server)
		if err != nil {
			return nil, fmt.Errorf("failed to shutdown server %s: %w", server.Name, err)
		}

		return action, nil
	})
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

	// Extract unique cluster names
	clusterSet := make(map[string]struct{})

	for _, server := range servers {
		if name, ok := server.Labels[LabelClusterName]; ok && name != "" {
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

// NodesExist returns true if nodes exist for the given cluster name.
func (p *Provider) NodesExist(ctx context.Context, clusterName string) (bool, error) {
	nodes, err := p.ListNodes(ctx, clusterName)
	if err != nil {
		return false, err
	}

	return len(nodes) > 0, nil
}

// DeleteNodes removes all servers for the given cluster.
func (p *Provider) DeleteNodes(ctx context.Context, clusterName string) error {
	if p.client == nil {
		return provider.ErrProviderUnavailable
	}

	nodes, err := p.ListNodes(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	for _, node := range nodes {
		server, _, err := p.client.Server.GetByName(ctx, node.Name)
		if err != nil {
			return fmt.Errorf("failed to get server %s: %w", node.Name, err)
		}

		if server == nil {
			continue
		}

		_, _, err = p.client.Server.DeleteWithResult(ctx, server)
		if err != nil {
			return fmt.Errorf("failed to delete server %s: %w", node.Name, err)
		}
	}

	// Clean up infrastructure resources
	err = p.deleteInfrastructure(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to delete infrastructure: %w", err)
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

// attachISOAndReboot attaches an ISO to a server and reboots it to boot from the ISO.
func (p *Provider) attachISOAndReboot(ctx context.Context, server *hcloud.Server, isoID int64) error {
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
	action, _, err = p.client.Server.Reset(ctx, server)
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
	ImageID          int64  // Image ID (for snapshots) - mutually exclusive with ISOID
	ISOID            int64  // ISO ID (for Talos public ISOs) - mutually exclusive with ImageID
	Location         string
	Labels           map[string]string
	UserData         string
	NetworkID        int64
	PlacementGroupID int64
	SSHKeyID         int64
	FirewallIDs      []int64
}

// EnsureNetwork ensures a network exists for the cluster, creating it if needed.
func (p *Provider) EnsureNetwork(
	ctx context.Context,
	clusterName string,
	cidr string,
) (*hcloud.Network, error) {
	if p.client == nil {
		return nil, provider.ErrProviderUnavailable
	}

	networkName := clusterName + NetworkSuffix

	// Check if network already exists
	network, _, err := p.client.Network.GetByName(ctx, networkName)
	if err != nil {
		return nil, fmt.Errorf("failed to get network %s: %w", networkName, err)
	}

	if network != nil {
		return network, nil
	}

	// Parse the network CIDR
	_, ipRange, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid network CIDR %s: %w", cidr, err)
	}

	network, _, err = p.client.Network.Create(ctx, hcloud.NetworkCreateOpts{
		Name:    networkName,
		IPRange: ipRange,
		Labels:  ResourceLabels(clusterName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create network %s: %w", networkName, err)
	}

	// Add a subnet for the servers
	subnetCIDR := "10.0.1.0/24"
	if cidr != "10.0.0.0/16" {
		// Use first /24 of the provided range
		subnetCIDR = cidr
	}

	_, subnetIPRange, err := net.ParseCIDR(subnetCIDR)
	if err != nil {
		return nil, fmt.Errorf("invalid subnet CIDR %s: %w", subnetCIDR, err)
	}

	_, _, err = p.client.Network.AddSubnet(ctx, network, hcloud.NetworkAddSubnetOpts{
		Subnet: hcloud.NetworkSubnet{
			Type:        hcloud.NetworkSubnetTypeCloud,
			NetworkZone: hcloud.NetworkZoneEUCentral,
			IPRange:     subnetIPRange,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to add subnet to network %s: %w", networkName, err)
	}

	return network, nil
}

// EnsureFirewall ensures a firewall exists for the cluster, creating it if needed.
func (p *Provider) EnsureFirewall(
	ctx context.Context,
	clusterName string,
) (*hcloud.Firewall, error) {
	if p.client == nil {
		return nil, provider.ErrProviderUnavailable
	}

	firewallName := clusterName + FirewallSuffix

	// Check if firewall already exists
	firewall, _, err := p.client.Firewall.GetByName(ctx, firewallName)
	if err != nil {
		return nil, fmt.Errorf("failed to get firewall %s: %w", firewallName, err)
	}

	if firewall != nil {
		return firewall, nil
	}

	// Create firewall with Talos-required rules
	result, _, err := p.client.Firewall.Create(ctx, hcloud.FirewallCreateOpts{
		Name:   firewallName,
		Labels: ResourceLabels(clusterName),
		Rules:  buildFirewallRules(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create firewall %s: %w", firewallName, err)
	}

	return result.Firewall, nil
}

// EnsurePlacementGroup ensures a placement group exists for the cluster.
func (p *Provider) EnsurePlacementGroup(
	ctx context.Context,
	clusterName string,
) (*hcloud.PlacementGroup, error) {
	if p.client == nil {
		return nil, provider.ErrProviderUnavailable
	}

	placementGroupName := clusterName + PlacementGroupSuffix

	// Check if placement group already exists
	placementGroup, _, err := p.client.PlacementGroup.GetByName(ctx, placementGroupName)
	if err != nil {
		return nil, fmt.Errorf("failed to get placement group %s: %w", placementGroupName, err)
	}

	if placementGroup != nil {
		return placementGroup, nil
	}

	// Create placement group with spread strategy for HA
	result, _, err := p.client.PlacementGroup.Create(ctx, hcloud.PlacementGroupCreateOpts{
		Name:   placementGroupName,
		Labels: ResourceLabels(clusterName),
		Type:   hcloud.PlacementGroupTypeSpread,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create placement group %s: %w", placementGroupName, err)
	}

	return result.PlacementGroup, nil
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

// forEachServer executes an action on each server in the cluster.
func (p *Provider) forEachServer(
	ctx context.Context,
	clusterName string,
	action func(*hcloud.Server) (*hcloud.Action, error),
) error {
	if p.client == nil {
		return provider.ErrProviderUnavailable
	}

	nodes, err := p.ListNodes(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	if len(nodes) == 0 {
		return provider.ErrNoNodes
	}

	for _, node := range nodes {
		server, _, serverErr := p.client.Server.GetByName(ctx, node.Name)
		if serverErr != nil {
			return fmt.Errorf("failed to get server %s: %w", node.Name, serverErr)
		}

		if server == nil {
			continue
		}

		hcloudAction, actionErr := action(server)
		if actionErr != nil {
			return actionErr
		}

		if hcloudAction != nil {
			waitErr := p.waitForAction(ctx, hcloudAction)
			if waitErr != nil {
				return waitErr
			}
		}
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
		StartAfterCreate: hcloud.Ptr(true),
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

// buildFirewallRules creates the firewall rules required for Talos.
func buildFirewallRules() []hcloud.FirewallRule {
	anyIP := []net.IPNet{
		{IP: net.ParseIP("0.0.0.0"), Mask: net.CIDRMask(0, 32)},
		{IP: net.ParseIP("::"), Mask: net.CIDRMask(0, 128)},
	}

	return []hcloud.FirewallRule{
		// Talos API (apid) - required for talosctl
		{
			Direction:   hcloud.FirewallRuleDirectionIn,
			Protocol:    hcloud.FirewallRuleProtocolTCP,
			Port:        hcloud.Ptr("50000"),
			SourceIPs:   anyIP,
			Description: hcloud.Ptr("Talos API (apid)"),
		},
		// Kubernetes API
		{
			Direction:   hcloud.FirewallRuleDirectionIn,
			Protocol:    hcloud.FirewallRuleProtocolTCP,
			Port:        hcloud.Ptr("6443"),
			SourceIPs:   anyIP,
			Description: hcloud.Ptr("Kubernetes API"),
		},
		// Talos trustd (for machine config)
		{
			Direction:   hcloud.FirewallRuleDirectionIn,
			Protocol:    hcloud.FirewallRuleProtocolTCP,
			Port:        hcloud.Ptr("50001"),
			SourceIPs:   anyIP,
			Description: hcloud.Ptr("Talos trustd"),
		},
		// etcd
		{
			Direction:   hcloud.FirewallRuleDirectionIn,
			Protocol:    hcloud.FirewallRuleProtocolTCP,
			Port:        hcloud.Ptr("2379-2380"),
			SourceIPs:   anyIP,
			Description: hcloud.Ptr("etcd"),
		},
		// Kubelet
		{
			Direction:   hcloud.FirewallRuleDirectionIn,
			Protocol:    hcloud.FirewallRuleProtocolTCP,
			Port:        hcloud.Ptr("10250"),
			SourceIPs:   anyIP,
			Description: hcloud.Ptr("Kubelet API"),
		},
		// ICMP (ping)
		{
			Direction:   hcloud.FirewallRuleDirectionIn,
			Protocol:    hcloud.FirewallRuleProtocolICMP,
			SourceIPs:   anyIP,
			Description: hcloud.Ptr("ICMP (ping)"),
		},
	}
}

// deleteInfrastructure cleans up infrastructure resources for a cluster.
func (p *Provider) deleteInfrastructure(ctx context.Context, clusterName string) error {
	// Delete placement group
	placementGroupName := clusterName + PlacementGroupSuffix

	placementGroup, _, err := p.client.PlacementGroup.GetByName(ctx, placementGroupName)
	if err == nil && placementGroup != nil {
		_, _ = p.client.PlacementGroup.Delete(ctx, placementGroup)
	}

	// Delete firewall
	firewallName := clusterName + FirewallSuffix

	firewall, _, err := p.client.Firewall.GetByName(ctx, firewallName)
	if err == nil && firewall != nil {
		_, _ = p.client.Firewall.Delete(ctx, firewall)
	}

	// Delete network
	networkName := clusterName + NetworkSuffix

	network, _, err := p.client.Network.GetByName(ctx, networkName)
	if err == nil && network != nil {
		_, _ = p.client.Network.Delete(ctx, network)
	}

	return nil
}
