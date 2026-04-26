package hetzner

import (
	"context"
	"fmt"
	"net"
	"slices"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
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
	if cidr != v1alpha1.DefaultHetznerNetworkCIDR {
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

// SyncFirewallRules updates an existing firewall's rules to match the current secure
// rule set. This migrates already-created clusters to the hardened configuration
// on the next `ksail cluster update`.
func (p *Provider) SyncFirewallRules(
	ctx context.Context,
	clusterName string,
) error {
	if p.client == nil {
		return provider.ErrProviderUnavailable
	}

	firewallName := clusterName + FirewallSuffix

	firewall, _, err := p.client.Firewall.GetByName(ctx, firewallName)
	if err != nil {
		return fmt.Errorf("failed to get firewall %s: %w", firewallName, err)
	}

	if firewall == nil {
		return nil // No firewall to sync
	}

	desiredRules := buildFirewallRules()

	// Skip update if rules already match
	if firewallRulesMatch(firewall.Rules, desiredRules) {
		return nil
	}

	_, _, err = p.client.Firewall.SetRules(ctx, firewall, hcloud.FirewallSetRulesOpts{
		Rules: desiredRules,
	})
	if err != nil {
		return fmt.Errorf("failed to sync firewall rules for %s: %w", firewallName, err)
	}

	return nil
}

// firewallRulesMatch returns true if existing and desired rules are equivalent,
// regardless of ordering. Rules are normalized by sorting before comparison so
// that rule ordering differences returned by the Hetzner API do not cause
// unnecessary SetRules calls.
func firewallRulesMatch(existing, desired []hcloud.FirewallRule) bool {
	if len(existing) != len(desired) {
		return false
	}

	sortedExisting := sortedFirewallRules(existing)
	sortedDesired := sortedFirewallRules(desired)

	for ruleIdx := range sortedExisting {
		if !firewallRulesEqual(sortedExisting[ruleIdx], sortedDesired[ruleIdx]) {
			return false
		}
	}

	return true
}

// firewallRulesEqual returns true if two firewall rules have identical fields.
func firewallRulesEqual(left, right hcloud.FirewallRule) bool {
	if left.Protocol != right.Protocol || left.Direction != right.Direction {
		return false
	}

	leftPort := ""
	if left.Port != nil {
		leftPort = *left.Port
	}

	rightPort := ""
	if right.Port != nil {
		rightPort = *right.Port
	}

	return leftPort == rightPort && sourceIPsEqual(left.SourceIPs, right.SourceIPs)
}

// sortedFirewallRules returns a sorted copy of rules, ordered by Direction → Protocol → Port.
func sortedFirewallRules(rules []hcloud.FirewallRule) []hcloud.FirewallRule {
	sorted := slices.Clone(rules)
	slices.SortFunc(sorted, compareFirewallRules)

	return sorted
}

// compareFirewallRules provides a stable sort order for firewall rules.
func compareFirewallRules(left, right hcloud.FirewallRule) int {
	if left.Direction != right.Direction {
		if left.Direction < right.Direction {
			return -1
		}

		return 1
	}

	if left.Protocol != right.Protocol {
		if left.Protocol < right.Protocol {
			return -1
		}

		return 1
	}

	leftPort := ""
	if left.Port != nil {
		leftPort = *left.Port
	}

	rightPort := ""
	if right.Port != nil {
		rightPort = *right.Port
	}

	if leftPort < rightPort {
		return -1
	}

	if leftPort > rightPort {
		return 1
	}

	return 0
}

// sourceIPsEqual returns true if two SourceIPs slices contain the same CIDRs,
// regardless of order.
func sourceIPsEqual(existing, desired []net.IPNet) bool {
	if len(existing) != len(desired) {
		return false
	}

	sortCIDRs := func(cidrs []net.IPNet) []string {
		strs := make([]string, len(cidrs))
		for i, c := range cidrs {
			strs[i] = c.String()
		}

		slices.Sort(strs)

		return strs
	}

	sortedExisting := sortCIDRs(existing)
	sortedDesired := sortCIDRs(desired)

	for idx := range sortedExisting {
		if sortedExisting[idx] != sortedDesired[idx] {
			return false
		}
	}

	return true
}

// EnsurePlacementGroup ensures a placement group exists for the cluster.
// If strategy is None, returns nil without creating a placement group.
// If customName is provided, uses that name; otherwise uses "<clusterName>-placement".
//
//nolint:nilnil // Intentional: nil,nil means "placement groups disabled, no error" - caller checks for nil group
func (p *Provider) EnsurePlacementGroup(
	ctx context.Context,
	clusterName string,
	strategy string,
	customName string,
) (*hcloud.PlacementGroup, error) {
	// Skip placement group creation if strategy is None
	if strategy == "None" || strategy == "" {
		return nil, nil
	}

	if p.client == nil {
		return nil, provider.ErrProviderUnavailable
	}

	placementGroupName := customName
	if placementGroupName == "" {
		placementGroupName = clusterName + PlacementGroupSuffix
	}

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

// buildFirewallRules creates the firewall rules for the public interface.
// Only ports that need public access are included (Talos API, Kubernetes API, ICMP).
// Cluster-internal ports (etcd, kubelet, trustd) are omitted because Hetzner Cloud
// Firewalls only filter public traffic — private network traffic is unfiltered.
func buildFirewallRules() []hcloud.FirewallRule {
	anyIP := []net.IPNet{
		{IP: net.ParseIP("0.0.0.0"), Mask: net.CIDRMask(0, IPv4CIDRBits)},
		{IP: net.ParseIP("::"), Mask: net.CIDRMask(0, IPv6CIDRBits)},
	}

	// Only expose ports that legitimately need public internet access.
	// Cluster-internal ports (etcd, kubelet, trustd) are NOT included because
	// Hetzner Cloud Firewalls only filter public interface traffic — inter-node
	// communication flows through the private Hetzner Cloud Network unfiltered.
	// See: https://docs.hetzner.com/cloud/firewalls/faq/
	// #do-firewalls-filter-traffic-between-servers-on-the-same-private-network
	return []hcloud.FirewallRule{
		// Talos API (apid) - required for talosctl
		{
			Direction:   hcloud.FirewallRuleDirectionIn,
			Protocol:    hcloud.FirewallRuleProtocolTCP,
			Port:        new("50000"),
			SourceIPs:   anyIP,
			Description: new("Talos API (apid)"),
		},
		// Kubernetes API
		{
			Direction:   hcloud.FirewallRuleDirectionIn,
			Protocol:    hcloud.FirewallRuleProtocolTCP,
			Port:        new("6443"),
			SourceIPs:   anyIP,
			Description: new("Kubernetes API"),
		},
		// ICMP (ping)
		{
			Direction:   hcloud.FirewallRuleDirectionIn,
			Protocol:    hcloud.FirewallRuleProtocolICMP,
			SourceIPs:   anyIP,
			Description: new("ICMP (ping)"),
		},
	}
}

// deleteInfrastructure cleans up infrastructure resources for a cluster.
func (p *Provider) deleteInfrastructure(ctx context.Context, clusterName string) error {
	// Small delay to ensure server deletions are fully processed
	time.Sleep(DefaultPreDeleteDelay)

	err := p.deletePlacementGroup(ctx, clusterName)
	if err != nil {
		return err
	}

	err = p.deleteFirewallWithRetry(ctx, clusterName)
	if err != nil {
		return err
	}

	return p.deleteNetwork(ctx, clusterName)
}

// deletePlacementGroup deletes the placement group for a cluster if it exists.
func (p *Provider) deletePlacementGroup(ctx context.Context, clusterName string) error {
	placementGroupName := clusterName + PlacementGroupSuffix

	placementGroup, err := retryTransientHetznerOperation(
		ctx,
		DefaultTransientRetryCount,
		p.calculateRetryDelay,
		func() (*hcloud.PlacementGroup, error) {
			placementGroup, _, err := p.client.PlacementGroup.GetByName(ctx, placementGroupName)
			if err != nil {
				return nil, fmt.Errorf(
					"failed to get placement group %s: %w",
					placementGroupName,
					err,
				)
			}

			return placementGroup, nil
		},
	)
	if err != nil {
		// Log error but don't fail - resource may not exist
		return nil //nolint:nilerr // Ignoring lookup error - resource may not exist
	}

	if placementGroup == nil {
		return nil
	}

	_, deleteErr := p.client.PlacementGroup.Delete(ctx, placementGroup)
	if deleteErr != nil {
		return fmt.Errorf("failed to delete placement group %s: %w", placementGroupName, deleteErr)
	}

	return nil
}

// deleteFirewallWithRetry deletes the firewall for a cluster with retry logic.
// Retries are needed because firewall may still be attached during server deletion.
func (p *Provider) deleteFirewallWithRetry(ctx context.Context, clusterName string) error {
	firewallName := clusterName + FirewallSuffix

	for attempt := range MaxDeleteRetries {
		firewall, err := retryTransientHetznerOperation(
			ctx,
			DefaultTransientRetryCount,
			p.calculateRetryDelay,
			func() (*hcloud.Firewall, error) {
				firewall, _, err := p.client.Firewall.GetByName(ctx, firewallName)
				if err != nil {
					return nil, fmt.Errorf("failed to get firewall %s: %w", firewallName, err)
				}

				return firewall, nil
			},
		)
		if err != nil {
			return nil //nolint:nilerr // Ignoring lookup error - resource may not exist
		}

		if firewall == nil {
			return nil
		}

		_, err = p.client.Firewall.Delete(ctx, firewall)
		if err == nil {
			return nil // Successfully deleted
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

	return nil
}

// deleteNetwork deletes the network for a cluster if it exists.
func (p *Provider) deleteNetwork(ctx context.Context, clusterName string) error {
	networkName := clusterName + NetworkSuffix

	network, err := retryTransientHetznerOperation(
		ctx,
		DefaultTransientRetryCount,
		p.calculateRetryDelay,
		func() (*hcloud.Network, error) {
			network, _, err := p.client.Network.GetByName(ctx, networkName)
			if err != nil {
				return nil, fmt.Errorf("failed to get network %s: %w", networkName, err)
			}

			return network, nil
		},
	)
	if err != nil {
		return nil //nolint:nilerr // Ignoring lookup error - resource may not exist
	}

	if network == nil {
		return nil
	}

	_, deleteErr := p.client.Network.Delete(ctx, network)
	if deleteErr != nil {
		return fmt.Errorf("failed to delete network %s: %w", networkName, deleteErr)
	}

	return nil
}
