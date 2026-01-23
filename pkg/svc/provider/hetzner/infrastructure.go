package hetzner

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provider"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

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

// buildFirewallRules creates the firewall rules required for Talos.
func buildFirewallRules() []hcloud.FirewallRule {
	anyIP := []net.IPNet{
		{IP: net.ParseIP("0.0.0.0"), Mask: net.CIDRMask(0, IPv4CIDRBits)},
		{IP: net.ParseIP("::"), Mask: net.CIDRMask(0, IPv6CIDRBits)},
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
//
//nolint:cyclop // Inherent complexity from deleting multiple resource types with retry logic
func (p *Provider) deleteInfrastructure(ctx context.Context, clusterName string) error {
	// Small delay to ensure server deletions are fully processed
	time.Sleep(DefaultPreDeleteDelay)

	// Delete placement group
	placementGroupName := clusterName + PlacementGroupSuffix

	placementGroup, _, err := p.client.PlacementGroup.GetByName(ctx, placementGroupName)
	if err == nil && placementGroup != nil {
		_, deleteErr := p.client.PlacementGroup.Delete(ctx, placementGroup)
		if deleteErr != nil {
			return fmt.Errorf(
				"failed to delete placement group %s: %w",
				placementGroupName,
				deleteErr,
			)
		}
	}

	// Delete firewall with retry (may still be attached during server deletion)
	firewallName := clusterName + FirewallSuffix

	for attempt := range MaxDeleteRetries {
		firewall, _, err := p.client.Firewall.GetByName(ctx, firewallName)
		if err != nil || firewall == nil {
			break // Firewall doesn't exist, we're done
		}

		_, err = p.client.Firewall.Delete(ctx, firewall)
		if err == nil {
			break // Successfully deleted
		}

		// If this is the last attempt, return the error
		if attempt == MaxDeleteRetries-1 {
			return fmt.Errorf(
				"failed to delete firewall %s after %d attempts: %w",
				firewallName,
				MaxDeleteRetries,
				err,
			)
		}

		// Wait before retrying
		time.Sleep(DefaultDeleteRetryDelay)
	}

	// Delete network
	networkName := clusterName + NetworkSuffix

	network, _, err := p.client.Network.GetByName(ctx, networkName)
	if err == nil && network != nil {
		_, deleteErr := p.client.Network.Delete(ctx, network)
		if deleteErr != nil {
			return fmt.Errorf("failed to delete network %s: %w", networkName, deleteErr)
		}
	}

	return nil
}
