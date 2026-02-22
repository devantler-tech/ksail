package hetzner_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provider/hetzner"
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
	firewall, err := prov.EnsureFirewall(context.TODO(), "test-cluster")

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
