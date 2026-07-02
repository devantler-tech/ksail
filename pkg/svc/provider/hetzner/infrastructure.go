package hetzner

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// EnsureNetwork ensures a network exists for the cluster, creating it if needed.
// The server subnet is reconciled on every call, so a Create retry after a partial
// failure (network created but AddSubnet not yet applied) heals the network instead
// of returning it without a subnet.
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

	if network == nil {
		// Parse the network CIDR
		_, ipRange, parseErr := net.ParseCIDR(cidr)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid network CIDR %s: %w", cidr, parseErr)
		}

		network, _, err = p.client.Network.Create(ctx, hcloud.NetworkCreateOpts{
			Name:    networkName,
			IPRange: ipRange,
			Labels:  ResourceLabels(clusterName),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create network %s: %w", networkName, err)
		}
	}

	err = p.ensureSubnet(ctx, network, networkName, cidr)
	if err != nil {
		return nil, err
	}

	return network, nil
}

// defaultServerSubnetCIDR is the historical server subnet carved out of the
// default network range; kept so existing default-CIDR clusters reconcile
// unchanged.
const defaultServerSubnetCIDR = "10.0.1.0/24"

// subnetMaskBits is the prefix length of the server subnet derived from a
// custom network range.
const subnetMaskBits = 24

// ipv4Bits is the bit length of an IPv4 mask, used to recognise IPv4 ranges.
const ipv4Bits = 32

// serverSubnetCIDR derives the server subnet from the network CIDR. The
// default network keeps its historical 10.0.1.0/24 subnet; a custom network
// uses its first /24 (a Hetzner subnet must sit inside the network range, and
// the full range would leave no room for further subnets). A custom range
// that is already /24 or smaller — or not IPv4 — is used whole.
func serverSubnetCIDR(cidr string) (string, error) {
	if cidr == v1alpha1.DefaultHetznerNetworkCIDR {
		return defaultServerSubnetCIDR, nil
	}

	_, ipRange, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", fmt.Errorf("invalid network CIDR %s: %w", cidr, err)
	}

	ones, bits := ipRange.Mask.Size()
	if bits != ipv4Bits || ones >= subnetMaskBits {
		return ipRange.String(), nil
	}

	firstSlash24 := net.IPNet{IP: ipRange.IP, Mask: net.CIDRMask(subnetMaskBits, bits)}

	return firstSlash24.String(), nil
}

// ensureSubnet adds the server subnet to the network when it is not already present.
// It reconciles a partial-failure state (network created but AddSubnet failed) on a
// retry and is idempotent: a subnet that already exists on the network is left as-is.
func (p *Provider) ensureSubnet(
	ctx context.Context,
	network *hcloud.Network,
	networkName string,
	cidr string,
) error {
	// Add a subnet for the servers
	subnetCIDR, err := serverSubnetCIDR(cidr)
	if err != nil {
		return err
	}

	_, subnetIPRange, err := net.ParseCIDR(subnetCIDR)
	if err != nil {
		return fmt.Errorf("invalid subnet CIDR %s: %w", subnetCIDR, err)
	}

	// Skip when the network already carries the subnet (idempotent reconcile).
	for _, subnet := range network.Subnets {
		if subnet.IPRange != nil && subnet.IPRange.String() == subnetIPRange.String() {
			return nil
		}
	}

	_, _, err = p.client.Network.AddSubnet(ctx, network, hcloud.NetworkAddSubnetOpts{
		Subnet: hcloud.NetworkSubnet{
			Type:        hcloud.NetworkSubnetTypeCloud,
			NetworkZone: hcloud.NetworkZoneEUCentral,
			IPRange:     subnetIPRange,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to add subnet to network %s: %w", networkName, err)
	}

	return nil
}

// EnsureFirewall ensures a firewall exists for the cluster, creating it if needed.
// When allowedCIDRs is non-empty, the Kubernetes API and Talos API firewall rules
// restrict source IPs to the specified CIDR blocks instead of 0.0.0.0/0 and ::/0.
func (p *Provider) EnsureFirewall(
	ctx context.Context,
	clusterName string,
	allowedCIDRs []string,
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
		Rules:  buildFirewallRules(allowedCIDRs),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create firewall %s: %w", firewallName, err)
	}

	return result.Firewall, nil
}

// SyncFirewallRules updates an existing firewall's rules to match the current secure
// rule set. This migrates already-created clusters to the hardened configuration
// on the next `ksail cluster update`.
// When allowedCIDRs is non-empty, the Kubernetes API and Talos API rules
// restrict source IPs to the specified CIDR blocks instead of 0.0.0.0/0 and ::/0.
func (p *Provider) SyncFirewallRules(
	ctx context.Context,
	clusterName string,
	allowedCIDRs []string,
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

	desiredRules := buildFirewallRules(allowedCIDRs)

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
//
// When allowedCIDRs is non-empty, the Kubernetes API (6443) and Talos API (50000) rules
// restrict source IPs to those CIDR blocks. ICMP remains open to all.
// When allowedCIDRs is empty, all rules use 0.0.0.0/0 and ::/0 (open to all).
func buildFirewallRules(allowedCIDRs []string) []hcloud.FirewallRule {
	anyIP := []net.IPNet{
		{IP: net.ParseIP("0.0.0.0"), Mask: net.CIDRMask(0, IPv4CIDRBits)},
		{IP: net.ParseIP("::"), Mask: net.CIDRMask(0, IPv6CIDRBits)},
	}

	// Determine source IPs for API ports: restricted CIDRs or open to all.
	apiSourceIPs := anyIP
	if len(allowedCIDRs) > 0 {
		apiSourceIPs = parseCIDRsToIPNets(allowedCIDRs)
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
			SourceIPs:   apiSourceIPs,
			Description: new("Talos API (apid)"),
		},
		// Kubernetes API
		{
			Direction:   hcloud.FirewallRuleDirectionIn,
			Protocol:    hcloud.FirewallRuleProtocolTCP,
			Port:        new("6443"),
			SourceIPs:   apiSourceIPs,
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

// parseCIDRsToIPNets converts a slice of CIDR strings to net.IPNet values.
// Invalid CIDRs are silently skipped (they should be validated before reaching here).
func parseCIDRsToIPNets(cidrs []string) []net.IPNet {
	result := make([]net.IPNet, 0, len(cidrs))

	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(strings.TrimSpace(cidr))
		if err != nil {
			continue
		}

		result = append(result, *ipNet)
	}

	return result
}

// deleteInfrastructure cleans up infrastructure resources for a cluster.
func (p *Provider) deleteInfrastructure(ctx context.Context, clusterName string) error {
	// Small delay to ensure server deletions are fully processed. The wait is
	// ctx-aware so Ctrl-C during cluster delete is honored instead of blocking.
	waitErr := waitForBackoff(
		ctx,
		"context cancelled before infrastructure deletion",
		DefaultPreDeleteDelay,
	)
	if waitErr != nil {
		return waitErr
	}

	err := p.deletePlacementGroup(ctx, clusterName)
	if err != nil {
		return err
	}

	err = p.deleteFirewallWithRetry(ctx, clusterName)
	if err != nil {
		return err
	}

	// Release the cluster's ksail-owned floating IP so `cluster delete` never
	// leaks a billed reserved address (server deletion only unassigns it).
	err = p.deleteFloatingIP(ctx, clusterName)
	if err != nil {
		return err
	}

	// Delete load balancers before the network — LBs attached to the network
	// must be removed first, otherwise network deletion will fail.
	err = p.deleteLoadBalancers(ctx, clusterName)
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

	return retryDelete(
		ctx,
		MaxDeleteRetries,
		DefaultDeleteRetryDelay,
		"context cancelled while retrying firewall deletion",
		func() (bool, error) {
			return p.attemptFirewallDelete(ctx, firewallName)
		},
		func(lastErr error) error {
			return fmt.Errorf(
				"failed to delete firewall %s after %d attempts: %w",
				firewallName,
				MaxDeleteRetries,
				lastErr,
			)
		},
	)
}

// attemptFirewallDelete performs one firewall lookup-and-delete attempt. It
// returns done=true on success or when the firewall is absent / its lookup
// fails (best-effort: the resource may not exist), and (false, err) when the
// delete call itself fails and should be retried.
func (p *Provider) attemptFirewallDelete(
	ctx context.Context,
	firewallName string,
) (bool, error) {
	firewall, err := retryTransientHetznerOperation(
		ctx,
		DefaultTransientRetryCount,
		p.calculateRetryDelay,
		func() (*hcloud.Firewall, error) {
			firewall, _, getErr := p.client.Firewall.GetByName(ctx, firewallName)
			if getErr != nil {
				return nil, fmt.Errorf("failed to get firewall %s: %w", firewallName, getErr)
			}

			return firewall, nil
		},
	)
	if err != nil || firewall == nil {
		//nolint:nilerr // Ignoring lookup error - resource may not exist.
		return true, nil // Resource may not exist; treat as done.
	}

	_, delErr := p.client.Firewall.Delete(ctx, firewall)
	if delErr == nil {
		return true, nil // Successfully deleted.
	}

	return false, delErr //nolint:wrapcheck // identity preserved
}

// getNetworkByClusterName looks up the cluster's private network with
// transient-retry. Returns (nil, nil) when the network does not exist.
func (p *Provider) getNetworkByClusterName(
	ctx context.Context,
	clusterName string,
) (*hcloud.Network, error) {
	networkName := clusterName + NetworkSuffix

	return retryTransientHetznerOperation(
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
}

// deleteNetwork deletes the network for a cluster if it exists.
func (p *Provider) deleteNetwork(ctx context.Context, clusterName string) error {
	networkName := clusterName + NetworkSuffix

	network, err := p.getNetworkByClusterName(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to look up network for deletion: %w", err)
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

// deleteLoadBalancers deletes all Hetzner Cloud Load Balancers attached to the
// cluster's private network. These are typically provisioned by hcloud-ccm for
// Kubernetes Service objects of type LoadBalancer.
//
// Load balancers must be deleted before the network, otherwise network deletion
// will fail because the network still has attached resources.
func (p *Provider) deleteLoadBalancers(ctx context.Context, clusterName string) error {
	// Look up the network with transient-retry to avoid silently skipping LB
	// cleanup on a transient API error (which could leave billed resources).
	network, err := p.getNetworkByClusterName(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to look up network for LB cleanup: %w", err)
	}

	if network == nil {
		return nil
	}

	loadBalancers, err := retryTransientHetznerOperation(
		ctx,
		DefaultTransientRetryCount,
		p.calculateRetryDelay,
		func() ([]*hcloud.LoadBalancer, error) {
			return p.client.LoadBalancer.AllWithOpts(ctx, hcloud.LoadBalancerListOpts{})
		},
	)
	if err != nil {
		return fmt.Errorf("failed to list load balancers: %w", err)
	}

	for _, lb := range loadBalancers {
		if !lbInNetwork(lb, network.Name) {
			continue
		}

		deleteErr := p.deleteLoadBalancerWithRetry(ctx, lb)
		if deleteErr != nil {
			return deleteErr
		}
	}

	return nil
}

// deleteLoadBalancerWithRetry deletes a single load balancer with retry logic.
// Retries handle the case where the LB is still processing actions from the
// recently deleted cluster.
func (p *Provider) deleteLoadBalancerWithRetry(
	ctx context.Context,
	loadBalancer *hcloud.LoadBalancer,
) error {
	return retryDelete(
		ctx,
		MaxDeleteRetries,
		DefaultDeleteRetryDelay,
		"context cancelled while retrying load balancer deletion",
		func() (bool, error) {
			_, err := p.client.LoadBalancer.Delete(ctx, loadBalancer)
			if err == nil {
				return true, nil
			}

			// The LB may have been deleted between list and delete (or is already
			// being removed). Treat not-found as success for idempotency.
			if hcloud.IsError(err, hcloud.ErrorCodeNotFound) {
				return true, nil
			}

			return false, err //nolint:wrapcheck // identity preserved
		},
		func(lastErr error) error {
			return fmt.Errorf(
				"failed to delete load balancer %s (ID %d) after %d attempts: %w",
				loadBalancer.Name, loadBalancer.ID, MaxDeleteRetries, lastErr,
			)
		},
	)
}

// lbInNetwork reports whether the given load balancer is attached to the named
// private network.
func lbInNetwork(lb *hcloud.LoadBalancer, networkName string) bool {
	for _, privateNet := range lb.PrivateNet {
		if privateNet.Network != nil && privateNet.Network.Name == networkName {
			return true
		}
	}

	return false
}
