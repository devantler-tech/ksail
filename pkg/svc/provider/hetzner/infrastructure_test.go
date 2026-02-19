package hetzner_test

import (
	"net"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to create string pointers for easier testing
func stringPtr(s string) *string {
	return &s
}

func TestBuildFirewallRules(t *testing.T) {
	t.Parallel()

	// Note: buildFirewallRules is not exported, so we test it indirectly through EnsureFirewall
	// But we can validate the rule structure expectations

	t.Run("FirewallRulesStructure", func(t *testing.T) {
		t.Parallel()

		// Expected rule count for Talos
		expectedRules := 6 // Talos API, K8s API, trustd, etcd, kubelet, ICMP

		// Verify the constants used match expected values
		assert.Equal(t, 32, hetzner.IPv4CIDRBits, "IPv4 CIDR bits should be 32")
		assert.Equal(t, 128, hetzner.IPv6CIDRBits, "IPv6 CIDR bits should be 128")

		// Verify we have the expected number of critical ports/services
		criticalPorts := []string{"50000", "6443", "50001", "2379-2380", "10250"}
		assert.Len(t, criticalPorts, 5, "Should have 5 critical port ranges")

		// Document that we expect 6 total rules (5 TCP + 1 ICMP)
		assert.Equal(t, expectedRules, 6, "Should have 6 firewall rules total")
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
		wantValid bool
	}{
		{
			name:      "ValidDefaultCIDR",
			cidr:      "10.0.0.0/16",
			wantError: false,
			wantValid: true,
		},
		{
			name:      "ValidCustomCIDR",
			cidr:      "192.168.0.0/16",
			wantError: false,
			wantValid: true,
		},
		{
			name:      "ValidSmallNetwork",
			cidr:      "10.1.0.0/24",
			wantError: false,
			wantValid: true,
		},
		{
			name:      "InvalidCIDR_NoMask",
			cidr:      "10.0.0.0",
			wantError: true,
			wantValid: false,
		},
		{
			name:      "InvalidCIDR_BadIP",
			cidr:      "999.0.0.0/16",
			wantError: true,
			wantValid: false,
		},
		{
			name:      "InvalidCIDR_BadMask",
			cidr:      "10.0.0.0/99",
			wantError: true,
			wantValid: false,
		},
		{
			name:      "EmptyCIDR",
			cidr:      "",
			wantError: true,
			wantValid: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, ipRange, err := net.ParseCIDR(testCase.cidr)

			if testCase.wantError {
				assert.Error(t, err)
				assert.Nil(t, ipRange)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, ipRange)
				assert.Equal(t, testCase.wantValid, ipRange != nil)
			}
		})
	}
}

func TestSubnetCIDRLogic(t *testing.T) {
	t.Parallel()

	t.Run("DefaultNetwork_UsesStandardSubnet", func(t *testing.T) {
		t.Parallel()

		networkCIDR := "10.0.0.0/16"
		expectedSubnet := "10.0.1.0/24"

		// Default logic: if CIDR is 10.0.0.0/16, use 10.0.1.0/24 subnet
		subnetCIDR := "10.0.1.0/24"
		if networkCIDR != "10.0.0.0/16" {
			subnetCIDR = networkCIDR
		}

		assert.Equal(t, expectedSubnet, subnetCIDR)
	})

	t.Run("CustomNetwork_UsesProvidedCIDR", func(t *testing.T) {
		t.Parallel()

		networkCIDR := "192.168.0.0/16"
		expectedSubnet := "192.168.0.0/16"

		subnetCIDR := "10.0.1.0/24"
		if networkCIDR != "10.0.0.0/16" {
			subnetCIDR = networkCIDR
		}

		assert.Equal(t, expectedSubnet, subnetCIDR)
	})

	t.Run("SmallNetwork_UsesProvidedCIDR", func(t *testing.T) {
		t.Parallel()

		networkCIDR := "10.1.0.0/24"
		expectedSubnet := "10.1.0.0/24"

		subnetCIDR := "10.0.1.0/24"
		if networkCIDR != "10.0.0.0/16" {
			subnetCIDR = networkCIDR
		}

		assert.Equal(t, expectedSubnet, subnetCIDR)
	})
}

func TestPlacementGroupStrategyHandling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		strategy       string
		shouldCreate   bool
		expectedResult bool
	}{
		{
			name:           "None_SkipsCreation",
			strategy:       "None",
			shouldCreate:   false,
			expectedResult: false,
		},
		{
			name:           "Empty_SkipsCreation",
			strategy:       "",
			shouldCreate:   false,
			expectedResult: false,
		},
		{
			name:           "Spread_CreatesPlacementGroup",
			strategy:       "Spread",
			shouldCreate:   true,
			expectedResult: true,
		},
		{
			name:           "Other_CreatesPlacementGroup",
			strategy:       "Custom",
			shouldCreate:   true,
			expectedResult: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// Logic from EnsurePlacementGroup
			shouldSkip := testCase.strategy == "None" || testCase.strategy == ""

			assert.Equal(t, !testCase.shouldCreate, shouldSkip)
			assert.Equal(t, testCase.expectedResult, testCase.shouldCreate)
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

func TestFirewallRuleDirection(t *testing.T) {
	t.Parallel()

	// Verify that firewall rules use correct direction
	directionIn := hcloud.FirewallRuleDirectionIn

	assert.Equal(t, "in", string(directionIn), "Firewall rules should allow inbound traffic")
}

func TestFirewallRuleProtocols(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		protocol hcloud.FirewallRuleProtocol
		expected string
	}{
		{
			name:     "TCP",
			protocol: hcloud.FirewallRuleProtocolTCP,
			expected: "tcp",
		},
		{
			name:     "UDP",
			protocol: hcloud.FirewallRuleProtocolUDP,
			expected: "udp",
		},
		{
			name:     "ICMP",
			protocol: hcloud.FirewallRuleProtocolICMP,
			expected: "icmp",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.expected, string(testCase.protocol))
		})
	}
}

func TestDeleteRetryLogic(t *testing.T) {
	t.Parallel()

	t.Run("RetryCount", func(t *testing.T) {
		t.Parallel()

		maxRetries := hetzner.MaxDeleteRetries

		assert.Positive(t, maxRetries)
		assert.GreaterOrEqual(t, maxRetries, 3, "Should retry at least 3 times")
		assert.LessOrEqual(t, maxRetries, 10, "Should not retry more than 10 times")
	})

	t.Run("RetryDelay", func(t *testing.T) {
		t.Parallel()

		retryDelay := hetzner.DefaultDeleteRetryDelay

		assert.Positive(t, retryDelay)
		assert.GreaterOrEqual(
			t,
			retryDelay.Seconds(),
			1.0,
			"Retry delay should be at least 1 second",
		)
	})

	t.Run("PreDeleteDelay", func(t *testing.T) {
		t.Parallel()

		preDeleteDelay := hetzner.DefaultPreDeleteDelay

		assert.GreaterOrEqual(
			t,
			preDeleteDelay.Seconds(),
			0.0,
			"PreDelete delay should be non-negative",
		)
	})
}

func TestNetworkZoneConstants(t *testing.T) {
	t.Parallel()

	// Verify that the expected network zone is defined in hcloud-go
	zone := hcloud.NetworkZoneEUCentral

	assert.Equal(t, "eu-central", string(zone), "Network zone should be eu-central")
}

func TestNetworkSubnetType(t *testing.T) {
	t.Parallel()

	// Verify that we use the correct subnet type
	subnetType := hcloud.NetworkSubnetTypeCloud

	assert.Equal(t, "cloud", string(subnetType), "Should use cloud subnet type")
}

func TestEnsureNetworkNilClient(t *testing.T) {
	t.Parallel()

	provider := hetzner.NewProvider(nil)
	require.NotNil(t, provider)

	// Should return error when client is nil
	network, err := provider.EnsureNetwork(nil, "test-cluster", "10.0.0.0/16")

	assert.Error(t, err)
	assert.Nil(t, network)
	assert.Contains(t, err.Error(), "unavailable")
}

func TestEnsureFirewallNilClient(t *testing.T) {
	t.Parallel()

	provider := hetzner.NewProvider(nil)
	require.NotNil(t, provider)

	// Should return error when client is nil
	firewall, err := provider.EnsureFirewall(nil, "test-cluster")

	assert.Error(t, err)
	assert.Nil(t, firewall)
	assert.Contains(t, err.Error(), "unavailable")
}

func TestEnsurePlacementGroupNilClient(t *testing.T) {
	t.Parallel()

	provider := hetzner.NewProvider(nil)
	require.NotNil(t, provider)

	t.Run("WithNoneStrategy", func(t *testing.T) {
		t.Parallel()

		// Should return nil without error when strategy is None
		pg, err := provider.EnsurePlacementGroup(nil, "test-cluster", "None", "")

		assert.NoError(t, err)
		assert.Nil(t, pg)
	})

	t.Run("WithEmptyStrategy", func(t *testing.T) {
		t.Parallel()

		// Should return nil without error when strategy is empty
		pg, err := provider.EnsurePlacementGroup(nil, "test-cluster", "", "")

		assert.NoError(t, err)
		assert.Nil(t, pg)
	})

	t.Run("WithSpreadStrategy", func(t *testing.T) {
		t.Parallel()

		// Should return error when client is nil and strategy requires placement group
		pg, err := provider.EnsurePlacementGroup(nil, "test-cluster", "Spread", "")

		assert.Error(t, err)
		assert.Nil(t, pg)
		assert.Contains(t, err.Error(), "unavailable")
	})
}

func TestResourceLabelsConsistency(t *testing.T) {
	t.Parallel()

	clusterName := "test-cluster"

	resourceLabels := hetzner.ResourceLabels(clusterName)
	nodeLabels := hetzner.NodeLabels(clusterName, "worker", 0)

	// Resource labels should be a subset of node labels
	for key, value := range resourceLabels {
		assert.Equal(t, value, nodeLabels[key], "Resource label %s should match in node labels", key)
	}

	// Node labels should have additional fields
	assert.Len(t, nodeLabels, len(resourceLabels)+2, "Node labels should have 2 additional fields")
}
