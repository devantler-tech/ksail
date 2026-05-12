package hetzner_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const defaultNetworkCIDR = "10.0.0.0/16"

func TestFirewallCIDRConfiguration(t *testing.T) {
	t.Parallel()

	t.Run("FirewallRulesStructure", func(t *testing.T) {
		t.Parallel()

		// Verify the constants used match expected values
		assert.Equal(t, 32, hetzner.IPv4CIDRBits, "IPv4 CIDR bits should be 32")
		assert.Equal(t, 128, hetzner.IPv6CIDRBits, "IPv6 CIDR bits should be 128")
	})

	t.Run("SourceIPRanges", func(t *testing.T) {
		t.Parallel()

		// Verify we can construct the expected source IP ranges
		anyIPv4 := net.IPNet{
			IP:   net.ParseIP("0.0.0.0"),
			Mask: net.CIDRMask(0, hetzner.IPv4CIDRBits),
		}
		assert.Equal(t, "0.0.0.0/0", anyIPv4.String(), "IPv4 any address")

		anyIPv6 := net.IPNet{
			IP:   net.ParseIP("::"),
			Mask: net.CIDRMask(0, hetzner.IPv6CIDRBits),
		}
		assert.Equal(t, "::/0", anyIPv6.String(), "IPv6 any address")
	})
}

func TestResourceNameConstruction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clusterName string
		suffix      string
		expected    string
	}{
		{
			name:        "NetworkName",
			clusterName: "test-cluster",
			suffix:      hetzner.NetworkSuffix,
			expected:    "test-cluster-network",
		},
		{
			name:        "FirewallName",
			clusterName: "prod-cluster",
			suffix:      hetzner.FirewallSuffix,
			expected:    "prod-cluster-firewall",
		},
		{
			name:        "PlacementGroupName",
			clusterName: "dev-cluster",
			suffix:      hetzner.PlacementGroupSuffix,
			expected:    "dev-cluster-placement",
		},
		{
			name:        "NameWithHyphens",
			clusterName: "my-production-cluster-1",
			suffix:      hetzner.NetworkSuffix,
			expected:    "my-production-cluster-1-network",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := testCase.clusterName + testCase.suffix
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestNetworkCIDRParsing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		cidr      string
		wantError bool
	}{
		{
			name:      "ValidDefaultCIDR",
			cidr:      "10.0.0.0/16",
			wantError: false,
		},
		{
			name:      "ValidCustomCIDR",
			cidr:      "192.168.0.0/16",
			wantError: false,
		},
		{
			name:      "ValidSmallNetwork",
			cidr:      "10.1.0.0/24",
			wantError: false,
		},
		{
			name:      "InvalidCIDR_NoMask",
			cidr:      "10.0.0.0",
			wantError: true,
		},
		{
			name:      "InvalidCIDR_BadIP",
			cidr:      "999.0.0.0/16",
			wantError: true,
		},
		{
			name:      "InvalidCIDR_BadMask",
			cidr:      "10.0.0.0/99",
			wantError: true,
		},
		{
			name:      "EmptyCIDR",
			cidr:      "",
			wantError: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, ipRange, err := net.ParseCIDR(testCase.cidr)

			if testCase.wantError {
				require.Error(t, err)
				assert.Nil(t, ipRange)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, ipRange)
			}
		})
	}
}

func TestPlacementGroupNaming(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clusterName string
		customName  string
		expected    string
	}{
		{
			name:        "DefaultNaming",
			clusterName: "test-cluster",
			customName:  "",
			expected:    "test-cluster-placement",
		},
		{
			name:        "CustomName",
			clusterName: "test-cluster",
			customName:  "my-custom-pg",
			expected:    "my-custom-pg",
		},
		{
			name:        "CustomNameOverridesDefault",
			clusterName: "prod-cluster",
			customName:  "prod-placement-custom",
			expected:    "prod-placement-custom",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// Logic from EnsurePlacementGroup
			placementGroupName := testCase.customName
			if placementGroupName == "" {
				placementGroupName = testCase.clusterName + hetzner.PlacementGroupSuffix
			}

			assert.Equal(t, testCase.expected, placementGroupName)
		})
	}
}

func TestDeleteRetryLogic(t *testing.T) {
	t.Parallel()

	t.Run("RetryCount", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, 5, hetzner.MaxDeleteRetries)
	})

	t.Run("RetryDelay", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, 2*time.Second, hetzner.DefaultDeleteRetryDelay)
	})

	t.Run("PreDeleteDelay", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, 5*time.Second, hetzner.DefaultPreDeleteDelay)
	})
}

func TestEnsureNetworkNilClient(t *testing.T) {
	t.Parallel()

	prov := hetzner.NewProvider(nil)
	require.NotNil(t, prov)

	// Should return error when client is nil
	network, err := prov.EnsureNetwork(context.TODO(), "test-cluster", defaultNetworkCIDR)

	require.Error(t, err)
	assert.Nil(t, network)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
}

func TestEnsureFirewallNilClient(t *testing.T) {
	t.Parallel()

	prov := hetzner.NewProvider(nil)
	require.NotNil(t, prov)

	// Should return error when client is nil
	firewall, err := prov.EnsureFirewall(context.TODO(), "test-cluster", nil)

	require.Error(t, err)
	assert.Nil(t, firewall)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
}

func TestEnsurePlacementGroupNilClient(t *testing.T) {
	t.Parallel()

	prov := hetzner.NewProvider(nil)
	require.NotNil(t, prov)

	t.Run("WithNoneStrategy", func(t *testing.T) {
		t.Parallel()

		// Should return nil without error when strategy is None
		pg, err := prov.EnsurePlacementGroup(context.TODO(), "test-cluster", "None", "")

		require.NoError(t, err)
		assert.Nil(t, pg)
	})

	t.Run("WithEmptyStrategy", func(t *testing.T) {
		t.Parallel()

		// Should return nil without error when strategy is empty
		pg, err := prov.EnsurePlacementGroup(context.TODO(), "test-cluster", "", "")

		require.NoError(t, err)
		assert.Nil(t, pg)
	})

	t.Run("WithSpreadStrategy", func(t *testing.T) {
		t.Parallel()

		// Should return error when client is nil and strategy requires placement group
		pg, err := prov.EnsurePlacementGroup(context.TODO(), "test-cluster", "Spread", "")

		require.Error(t, err)
		assert.Nil(t, pg)
		require.ErrorIs(t, err, provider.ErrProviderUnavailable)
	})
}

func TestSyncFirewallRulesNilClient(t *testing.T) {
	t.Parallel()

	prov := hetzner.NewProvider(nil)
	require.NotNil(t, prov)

	// Should return error when client is nil
	err := prov.SyncFirewallRules(context.TODO(), "test-cluster", nil)

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
}

func TestResourceLabelsConsistency(t *testing.T) {
	t.Parallel()

	clusterName := "test-cluster"

	resourceLabels := hetzner.ResourceLabels(clusterName)
	nodeLabels := hetzner.NodeLabels(clusterName, hetzner.NodeTypeWorker, 0)

	// Resource labels should be a subset of node labels
	for key, value := range resourceLabels {
		assert.Equal(
			t,
			value,
			nodeLabels[key],
			"Resource label %s should match in node labels",
			key,
		)
	}

	// Node labels should have additional fields
	assert.Len(t, nodeLabels, len(resourceLabels)+2, "Node labels should have 2 additional fields")
}

// makeTCPRule constructs a minimal inbound TCP FirewallRule for test fixtures.
func makeTCPRule(port string, sourceIPs []net.IPNet) hcloud.FirewallRule {
	return hcloud.FirewallRule{
		Direction: hcloud.FirewallRuleDirectionIn,
		Protocol:  hcloud.FirewallRuleProtocolTCP,
		Port:      &port,
		SourceIPs: sourceIPs,
	}
}

func TestFirewallRulesMatch(t *testing.T) { //nolint:funlen // table-driven test requires many cases
	t.Parallel()

	anyIP := []net.IPNet{
		{IP: net.ParseIP("0.0.0.0"), Mask: net.CIDRMask(0, hetzner.IPv4CIDRBits)},
		{IP: net.ParseIP("::"), Mask: net.CIDRMask(0, hetzner.IPv6CIDRBits)},
	}

	ruleA := makeTCPRule("50000", anyIP)
	ruleB := makeTCPRule("6443", anyIP)

	tests := []struct {
		name     string
		existing []hcloud.FirewallRule
		desired  []hcloud.FirewallRule
		want     bool
	}{
		{
			name:     "EmptySlices",
			existing: []hcloud.FirewallRule{},
			desired:  []hcloud.FirewallRule{},
			want:     true,
		},
		{
			name:     "IdenticalSingleRule",
			existing: []hcloud.FirewallRule{ruleA},
			desired:  []hcloud.FirewallRule{ruleA},
			want:     true,
		},
		{
			name:     "IdenticalOrderPreserved",
			existing: []hcloud.FirewallRule{ruleA, ruleB},
			desired:  []hcloud.FirewallRule{ruleA, ruleB},
			want:     true,
		},
		{
			name:     "OrderIndependent",
			existing: []hcloud.FirewallRule{ruleB, ruleA},
			desired:  []hcloud.FirewallRule{ruleA, ruleB},
			want:     true,
		},
		{
			name:     "DifferentCount",
			existing: []hcloud.FirewallRule{ruleA},
			desired:  []hcloud.FirewallRule{ruleA, ruleB},
			want:     false,
		},
		{
			name:     "DifferentPort",
			existing: []hcloud.FirewallRule{makeTCPRule("8080", anyIP)},
			desired:  []hcloud.FirewallRule{makeTCPRule("9090", anyIP)},
			want:     false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := hetzner.FirewallRulesMatchForTest(testCase.existing, testCase.desired)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestSourceIPsEqual(t *testing.T) {
	t.Parallel()

	cidrA := net.IPNet{IP: net.ParseIP("0.0.0.0"), Mask: net.CIDRMask(0, hetzner.IPv4CIDRBits)}
	cidrB := net.IPNet{IP: net.ParseIP("::"), Mask: net.CIDRMask(0, hetzner.IPv6CIDRBits)}
	cidrC := net.IPNet{IP: net.ParseIP("10.0.0.0"), Mask: net.CIDRMask(16, hetzner.IPv4CIDRBits)}

	tests := []struct {
		name     string
		existing []net.IPNet
		desired  []net.IPNet
		want     bool
	}{
		{
			name:     "EmptySlices",
			existing: []net.IPNet{},
			desired:  []net.IPNet{},
			want:     true,
		},
		{
			name:     "SingleCIDRMatch",
			existing: []net.IPNet{cidrA},
			desired:  []net.IPNet{cidrA},
			want:     true,
		},
		{
			name:     "OrderIndependent",
			existing: []net.IPNet{cidrB, cidrA},
			desired:  []net.IPNet{cidrA, cidrB},
			want:     true,
		},
		{
			name:     "DifferentCIDR",
			existing: []net.IPNet{cidrA},
			desired:  []net.IPNet{cidrC},
			want:     false,
		},
		{
			name:     "DifferentCount",
			existing: []net.IPNet{cidrA},
			desired:  []net.IPNet{cidrA, cidrB},
			want:     false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := hetzner.SourceIPsEqualForTest(testCase.existing, testCase.desired)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestBuildFirewallRulesSecureSet(t *testing.T) {
	t.Parallel()

	rules := hetzner.BuildFirewallRulesForTest(nil)

	// Only Talos API, Kubernetes API, and ICMP should be exposed publicly.
	// Cluster-internal ports (etcd 2379-2380, kubelet 10250, trustd 50001)
	// must NOT appear in the secure rule set.
	require.Len(t, rules, 3, "secure rule set must have exactly 3 rules")

	ports := make(map[string]bool)
	protocols := make(map[hcloud.FirewallRuleProtocol]bool)

	for _, rule := range rules {
		assert.Equal(t, hcloud.FirewallRuleDirectionIn, rule.Direction, "all rules must be inbound")

		if rule.Port != nil {
			ports[*rule.Port] = true
		}

		protocols[rule.Protocol] = true

		// Every rule must be open to 0.0.0.0/0 and ::/0
		require.Len(
			t,
			rule.SourceIPs,
			2,
			"each rule must list both IPv4 and IPv6 any-address CIDRs",
		)
	}

	assert.True(t, ports["50000"], "Talos API port 50000 must be present")
	assert.True(t, ports["6443"], "Kubernetes API port 6443 must be present")
	assert.True(t, protocols[hcloud.FirewallRuleProtocolICMP], "ICMP must be present for ping")

	// Ensure no insecure internal ports are exposed
	assert.False(t, ports["2379"], "etcd port 2379 must NOT be exposed publicly")
	assert.False(t, ports["2380"], "etcd port 2380 must NOT be exposed publicly")
	assert.False(t, ports["10250"], "kubelet port 10250 must NOT be exposed publicly")
	assert.False(t, ports["50001"], "trustd port 50001 must NOT be exposed publicly")
}

func TestBuildFirewallRulesWithAllowedCIDRs(t *testing.T) {
	t.Parallel()

	allowedCIDRs := []string{"203.0.113.0/24", "198.51.100.0/24"}
	rules := hetzner.BuildFirewallRulesForTest(allowedCIDRs)

	require.Len(t, rules, 3, "secure rule set must have exactly 3 rules")

	for _, rule := range rules {
		assert.Equal(t, hcloud.FirewallRuleDirectionIn, rule.Direction, "all rules must be inbound")

		if rule.Protocol == hcloud.FirewallRuleProtocolICMP {
			// ICMP must remain open to all regardless of allowed CIDRs
			require.Len(t, rule.SourceIPs, 2, "ICMP must stay open to 0.0.0.0/0 and ::/0")
		} else {
			// TCP rules (ports 50000, 6443) must use the provided CIDRs
			require.Len(t, rule.SourceIPs, 2, "API rules must use the provided CIDRs")

			sourceStrs := make([]string, 0, len(rule.SourceIPs))
			for _, src := range rule.SourceIPs {
				sourceStrs = append(sourceStrs, src.String())
			}

			assert.Contains(t, sourceStrs, "203.0.113.0/24", "must contain first allowed CIDR")
			assert.Contains(t, sourceStrs, "198.51.100.0/24", "must contain second allowed CIDR")
			assert.NotContains(t, sourceStrs, "0.0.0.0/0", "must NOT contain open IPv4 CIDR")
			assert.NotContains(t, sourceStrs, "::/0", "must NOT contain open IPv6 CIDR")
		}
	}
}

type lbInNetworkTestCase struct {
	name        string
	lb          *hcloud.LoadBalancer
	networkName string
	expected    bool
}

func lbInNetworkTestCases() []lbInNetworkTestCase {
	mkNet := func(name string) *hcloud.Network {
		return &hcloud.Network{Name: name}
	}
	mkPrivNets := func(nets ...*hcloud.Network) []hcloud.LoadBalancerPrivateNet {
		out := make([]hcloud.LoadBalancerPrivateNet, len(nets))
		for i, n := range nets {
			out[i] = hcloud.LoadBalancerPrivateNet{Network: n}
		}

		return out
	}
	mkLB := func(nets []hcloud.LoadBalancerPrivateNet) *hcloud.LoadBalancer {
		return &hcloud.LoadBalancer{PrivateNet: nets}
	}
	cluster := "my-cluster-network"

	return []lbInNetworkTestCase{
		{"MatchingNetwork", mkLB(mkPrivNets(mkNet(cluster))), cluster, true},
		{"DifferentNetwork", mkLB(mkPrivNets(mkNet("other"))), cluster, false},
		{"NoPrivateNetworks", mkLB(nil), cluster, false},
		{"EmptyPrivateNetworks", mkLB(mkPrivNets()), cluster, false},
		{"MultipleNetworks_OneMatching",
			mkLB(mkPrivNets(mkNet("other"), mkNet(cluster))), cluster, true},
		{"NilNetworkField", mkLB(mkPrivNets(nil)), cluster, false},
	}
}

func TestLBInNetwork(t *testing.T) {
	t.Parallel()

	for _, testCase := range lbInNetworkTestCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := hetzner.LBInNetworkForTest(testCase.lb, testCase.networkName)
			assert.Equal(t, testCase.expected, result)
		})
	}
}
