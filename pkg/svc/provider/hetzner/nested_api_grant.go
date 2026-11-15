package hetzner

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

const (
	// nestedAPIRuleDescriptionPrefix marks a host firewall rule that admits the API of one
	// nested cluster. The nested cluster's name follows the prefix, so a rule belongs to
	// exactly one nested cluster and host updates can tell it apart from the base rules.
	nestedAPIRuleDescriptionPrefix = "KSail nested API: "
	// kubernetesAPIPort is the host's own Kubernetes API port. Its rule's sources are the
	// host's access restriction, which every nested API grant inherits.
	kubernetesAPIPort = "6443"
	maxPort           = 65535
)

// nestedAPIRuleDescription returns the description that identifies the grant for nestedCluster.
func nestedAPIRuleDescription(nestedCluster string) string {
	return nestedAPIRuleDescriptionPrefix + nestedCluster
}

// isNestedAPIRule reports whether rule is a nested cluster's API grant.
func isNestedAPIRule(rule hcloud.FirewallRule) bool {
	return rule.Description != nil &&
		strings.HasPrefix(*rule.Description, nestedAPIRuleDescriptionPrefix)
}

// isNestedAPIRuleFor reports whether rule is the API grant of nestedCluster.
func isNestedAPIRuleFor(rule hcloud.FirewallRule, nestedCluster string) bool {
	return rule.Description != nil && *rule.Description == nestedAPIRuleDescription(nestedCluster)
}

// kubernetesAPISourceIPs returns the sources of the inbound TCP rule for the host's
// Kubernetes API, and false when rules hold no such rule.
func kubernetesAPISourceIPs(rules []hcloud.FirewallRule) ([]net.IPNet, bool) {
	for _, rule := range rules {
		if rule.Direction == hcloud.FirewallRuleDirectionIn &&
			rule.Protocol == hcloud.FirewallRuleProtocolTCP &&
			rule.Port != nil && *rule.Port == kubernetesAPIPort &&
			!isNestedAPIRule(rule) {
			return rule.SourceIPs, true
		}
	}

	return nil, false
}

// withNestedAPIGrants returns baseRules followed by every nested API grant in existingRules.
// Each kept grant takes the sources of the Kubernetes API rule in baseRules, so a change to
// the host's allowed CIDRs applies to its nested grants too and never leaves one wider than
// the host's own API. When baseRules has no Kubernetes API rule, the grants are dropped
// rather than kept with sources nothing restricts.
func withNestedAPIGrants(baseRules, existingRules []hcloud.FirewallRule) []hcloud.FirewallRule {
	sources, ok := kubernetesAPISourceIPs(baseRules)
	if !ok {
		return baseRules
	}

	rules := append([]hcloud.FirewallRule(nil), baseRules...)

	for _, rule := range existingRules {
		if !isNestedAPIRule(rule) {
			continue
		}

		grant := rule

		grant.SourceIPs = append([]net.IPNet(nil), sources...)
		rules = append(rules, grant)
	}

	return rules
}

// GrantNestedAPIPort admits inbound TCP traffic to port on the KSail-owned firewall of
// hostCluster, for the API of the nested cluster nestedCluster. The grant takes the sources
// of the host's Kubernetes API rule, so it is never reachable from more addresses than the
// host's own API. Granting again for the same nested cluster replaces its earlier grant;
// other nested clusters' grants are kept. It returns once the rules are in effect.
func (p *Provider) GrantNestedAPIPort(
	ctx context.Context,
	hostCluster string,
	nestedCluster string,
	port int,
) error {
	if nestedCluster == "" {
		return fmt.Errorf("%w: nested cluster name is required", ErrInvalidNestedAPIGrant)
	}

	if port < 1 || port > maxPort {
		return fmt.Errorf("%w: port %d is outside 1-%d", ErrInvalidNestedAPIGrant, port, maxPort)
	}

	defer lockFirewallRules(hostCluster + FirewallSuffix)()

	firewall, err := p.ownedHostFirewall(ctx, hostCluster)
	if err != nil {
		return err
	}

	sources, ok := kubernetesAPISourceIPs(firewall.Rules)
	if !ok {
		return fmt.Errorf("%w: %s", ErrNoKubernetesAPIRule, firewall.Name)
	}

	rules := withoutNestedAPIGrant(firewall.Rules, nestedCluster)
	rules = append(rules, hcloud.FirewallRule{
		Direction:   hcloud.FirewallRuleDirectionIn,
		Protocol:    hcloud.FirewallRuleProtocolTCP,
		Port:        new(strconv.Itoa(port)),
		SourceIPs:   append([]net.IPNet(nil), sources...),
		Description: new(nestedAPIRuleDescription(nestedCluster)),
	})

	return p.setRulesIfChanged(ctx, firewall, rules)
}

// RevokeNestedAPIPort removes the API grant of nestedCluster from the KSail-owned firewall
// of hostCluster and leaves every other rule in place. It is a no-op when the firewall or
// the grant does not exist.
func (p *Provider) RevokeNestedAPIPort(
	ctx context.Context,
	hostCluster string,
	nestedCluster string,
) error {
	if nestedCluster == "" {
		return fmt.Errorf("%w: nested cluster name is required", ErrInvalidNestedAPIGrant)
	}

	defer lockFirewallRules(hostCluster + FirewallSuffix)()

	firewall, err := p.ownedHostFirewall(ctx, hostCluster)
	if errors.Is(err, ErrHostFirewallNotFound) {
		return nil
	}

	if err != nil {
		return err
	}

	rules := withoutNestedAPIGrant(firewall.Rules, nestedCluster)
	if len(rules) == len(firewall.Rules) {
		return nil
	}

	return p.setRulesIfChanged(ctx, firewall, rules)
}

// ownedHostFirewall returns the firewall of hostCluster, or ErrHostFirewallNotFound when it
// does not exist.
// A firewall with that name that KSail does not own for hostCluster is an error, so a
// manually managed firewall is never changed.
func (p *Provider) ownedHostFirewall(
	ctx context.Context,
	hostCluster string,
) (*hcloud.Firewall, error) {
	if p.client == nil {
		return nil, provider.ErrProviderUnavailable
	}

	firewallName := hostCluster + FirewallSuffix

	firewall, _, err := p.client.Firewall.GetByName(ctx, firewallName)
	if err != nil {
		return nil, fmt.Errorf("failed to get firewall %s: %w", firewallName, err)
	}

	if firewall == nil {
		return nil, fmt.Errorf("%w: %s", ErrHostFirewallNotFound, firewallName)
	}

	if firewall.Labels[LabelOwned] != LabelOwnedValue ||
		firewall.Labels[LabelClusterName] != hostCluster {
		return nil, fmt.Errorf(
			"%w: %s (cluster %s)",
			ErrFirewallNotOwned,
			firewallName,
			hostCluster,
		)
	}

	return firewall, nil
}

// withoutNestedAPIGrant returns a copy of rules without the API grant of nestedCluster.
func withoutNestedAPIGrant(
	rules []hcloud.FirewallRule,
	nestedCluster string,
) []hcloud.FirewallRule {
	kept := make([]hcloud.FirewallRule, 0, len(rules))

	for _, rule := range rules {
		if !isNestedAPIRuleFor(rule, nestedCluster) {
			kept = append(kept, rule)
		}
	}

	return kept
}
