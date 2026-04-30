//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package hetzner

import "time"

// FirewallRulesMatchForTest exports firewallRulesMatch for testing.
var FirewallRulesMatchForTest = firewallRulesMatch

// SourceIPsEqualForTest exports sourceIPsEqual for testing.
var SourceIPsEqualForTest = sourceIPsEqual

// BuildFirewallRulesForTest exports buildFirewallRules for testing.
var BuildFirewallRulesForTest = buildFirewallRules

// ShouldRetryErrorForTest exports shouldRetryError for testing.
var ShouldRetryErrorForTest = shouldRetryError

// ShouldDisablePlacementForTest exports shouldDisablePlacement for testing.
var ShouldDisablePlacementForTest = shouldDisablePlacement

// CalculateRetryDelayForTest exposes calculateRetryDelay for testing via a nil-client provider.
// Only the attempt number matters; the client field is unused by this method.
func CalculateRetryDelayForTest(attempt int) time.Duration {
	p := &Provider{}

	return p.calculateRetryDelay(attempt)
}
