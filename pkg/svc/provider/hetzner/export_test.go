//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package hetzner

import "github.com/hetznercloud/hcloud-go/v2/hcloud"

// FirewallRulesMatchForTest exports firewallRulesMatch for testing.
var FirewallRulesMatchForTest = firewallRulesMatch

// SourceIPsEqualForTest exports sourceIPsEqual for testing.
var SourceIPsEqualForTest = sourceIPsEqual

// BuildFirewallRulesForTest exports buildFirewallRules for testing.
var BuildFirewallRulesForTest = func() []hcloud.FirewallRule {
	return buildFirewallRules()
}
