package hetzner_test

import (
	"net"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProvider(t *testing.T) {
	t.Parallel()

	t.Run("WithClient", func(t *testing.T) {
		t.Parallel()

		client := hcloud.NewClient(hcloud.WithToken("test-token"))
		prov := hetzner.NewProvider(client)

		require.NotNil(t, prov)
		assert.True(t, prov.IsAvailable())
	})

	t.Run("WithNilClient", func(t *testing.T) {
		t.Parallel()

		prov := hetzner.NewProvider(nil)

		require.NotNil(t, prov)
		assert.False(t, prov.IsAvailable())
	})
}

func TestNewProviderFromToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		token     string
		available bool
	}{
		{
			name:      "WithToken",
			token:     "test-token",
			available: true,
		},
		{
			name:      "WithEmptyToken",
			token:     "",
			available: true, // Provider is created, but API calls would fail
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			prov := hetzner.NewProviderFromToken(testCase.token)

			require.NotNil(t, prov)
			assert.Equal(t, testCase.available, prov.IsAvailable())
		})
	}
}

func TestIsAvailable(t *testing.T) {
	t.Parallel()

	t.Run("WithValidClient", func(t *testing.T) {
		t.Parallel()

		client := hcloud.NewClient(hcloud.WithToken("test"))
		prov := hetzner.NewProvider(client)

		assert.True(t, prov.IsAvailable())
	})

	t.Run("WithNilClient", func(t *testing.T) {
		t.Parallel()

		prov := hetzner.NewProvider(nil)

		assert.False(t, prov.IsAvailable())
	})
}

func TestLabelConstants(t *testing.T) {
	t.Parallel()

	t.Run("LabelOwned", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "ksail.owned", hetzner.LabelOwned)
	})

	t.Run("LabelClusterName", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "ksail.cluster.name", hetzner.LabelClusterName)
	})

	t.Run("LabelNodeType", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "ksail.node.type", hetzner.LabelNodeType)
	})

	t.Run("LabelNodeIndex", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "ksail.node.index", hetzner.LabelNodeIndex)
	})
}

func TestNodeTypeConstants(t *testing.T) {
	t.Parallel()

	t.Run("NodeTypeControlPlane", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "controlplane", hetzner.NodeTypeControlPlane)
	})

	t.Run("NodeTypeWorker", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "worker", hetzner.NodeTypeWorker)
	})
}

func TestResourceSuffixConstants(t *testing.T) {
	t.Parallel()

	t.Run("NetworkSuffix", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "-network", hetzner.NetworkSuffix)
	})

	t.Run("FirewallSuffix", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "-firewall", hetzner.FirewallSuffix)
	})

	t.Run("PlacementGroupSuffix", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "-placement", hetzner.PlacementGroupSuffix)
	})
}

func TestResourceLabels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clusterName string
		wantLabels  map[string]string
	}{
		{
			name:        "BasicCluster",
			clusterName: "my-cluster",
			wantLabels: map[string]string{
				hetzner.LabelOwned:       "true",
				hetzner.LabelClusterName: "my-cluster",
			},
		},
		{
			name:        "ClusterWithHyphens",
			clusterName: "production-cluster-1",
			wantLabels: map[string]string{
				hetzner.LabelOwned:       "true",
				hetzner.LabelClusterName: "production-cluster-1",
			},
		},
		{
			name:        "EmptyClusterName",
			clusterName: "",
			wantLabels: map[string]string{
				hetzner.LabelOwned:       "true",
				hetzner.LabelClusterName: "",
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			labels := hetzner.ResourceLabels(testCase.clusterName)

			assert.Equal(t, testCase.wantLabels, labels)
			assert.Len(t, labels, 2)
		})
	}
}

func TestNodeLabels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clusterName string
		nodeType    string
		index       int
		wantLabels  map[string]string
	}{
		{
			name:        "ControlPlaneNode0",
			clusterName: "my-cluster",
			nodeType:    hetzner.NodeTypeControlPlane,
			index:       0,
			wantLabels: map[string]string{
				hetzner.LabelOwned:       "true",
				hetzner.LabelClusterName: "my-cluster",
				hetzner.LabelNodeType:    "controlplane",
				hetzner.LabelNodeIndex:   "0",
			},
		},
		{
			name:        "WorkerNode3",
			clusterName: "prod-cluster",
			nodeType:    hetzner.NodeTypeWorker,
			index:       3,
			wantLabels: map[string]string{
				hetzner.LabelOwned:       "true",
				hetzner.LabelClusterName: "prod-cluster",
				hetzner.LabelNodeType:    "worker",
				hetzner.LabelNodeIndex:   "3",
			},
		},
		{
			name:        "CustomNodeType",
			clusterName: "test",
			nodeType:    "custom",
			index:       1,
			wantLabels: map[string]string{
				hetzner.LabelOwned:       "true",
				hetzner.LabelClusterName: "test",
				hetzner.LabelNodeType:    "custom",
				hetzner.LabelNodeIndex:   "1",
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			labels := hetzner.NodeLabels(testCase.clusterName, testCase.nodeType, testCase.index)

			assert.Equal(t, testCase.wantLabels, labels)
			assert.Len(t, labels, 4)
		})
	}
}

func TestConstantsAreDistinct(t *testing.T) {
	t.Parallel()

	labels := []string{
		hetzner.LabelOwned,
		hetzner.LabelClusterName,
		hetzner.LabelNodeType,
		hetzner.LabelNodeIndex,
	}

	seen := make(map[string]bool)

	for _, label := range labels {
		assert.False(t, seen[label], "duplicate label constant: %s", label)
		seen[label] = true
	}
}

func TestResourceNaming(t *testing.T) {
	t.Parallel()

	clusterName := "my-cluster"

	t.Run("NetworkName", func(t *testing.T) {
		t.Parallel()

		name := clusterName + hetzner.NetworkSuffix
		assert.Equal(t, "my-cluster-network", name)
	})

	t.Run("FirewallName", func(t *testing.T) {
		t.Parallel()

		name := clusterName + hetzner.FirewallSuffix
		assert.Equal(t, "my-cluster-firewall", name)
	})

	t.Run("PlacementGroupName", func(t *testing.T) {
		t.Parallel()

		name := clusterName + hetzner.PlacementGroupSuffix
		assert.Equal(t, "my-cluster-placement", name)
	})
}

func TestDefaultTimeoutConstants(t *testing.T) {
	t.Parallel()

	// Verify timeouts are reasonable values
	assert.Positive(t, hetzner.DefaultActionTimeout, "DefaultActionTimeout should be positive")
	assert.Positive(
		t,
		hetzner.DefaultOperationTimeout,
		"DefaultOperationTimeout should be positive",
	)
	assert.Positive(t, hetzner.DefaultPollingInterval, "DefaultPollingInterval should be positive")
	assert.Positive(
		t,
		hetzner.DefaultDeleteRetryDelay,
		"DefaultDeleteRetryDelay should be positive",
	)
	assert.Positive(t, hetzner.DefaultPreDeleteDelay, "DefaultPreDeleteDelay should be positive")

	// Verify action timeout is longer than operation timeout
	assert.GreaterOrEqual(
		t,
		hetzner.DefaultActionTimeout,
		hetzner.DefaultOperationTimeout,
		"DefaultActionTimeout should be >= DefaultOperationTimeout",
	)
}

func TestCIDRBitConstants(t *testing.T) {
	t.Parallel()

	t.Run("IPv4CIDRBits", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, 32, hetzner.IPv4CIDRBits)
	})

	t.Run("IPv6CIDRBits", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, 128, hetzner.IPv6CIDRBits)
	})
}

func TestMaxDeleteRetries(t *testing.T) {
	t.Parallel()

	assert.Positive(t, hetzner.MaxDeleteRetries, "MaxDeleteRetries should be positive")
	assert.LessOrEqual(t, hetzner.MaxDeleteRetries, 10, "MaxDeleteRetries should be reasonable")
}

func TestErrHetznerActionFailed(t *testing.T) {
	t.Parallel()

	require.Error(t, hetzner.ErrHetznerActionFailed)
	assert.Contains(t, hetzner.ErrHetznerActionFailed.Error(), "hetzner action failed")
}

func TestCIDRMaskCreation(t *testing.T) {
	t.Parallel()

	t.Run("IPv4AnyAddress", func(t *testing.T) {
		t.Parallel()

		// Test that IPv4 0.0.0.0/0 can be created with our constants
		ipNet := net.IPNet{
			IP:   net.ParseIP("0.0.0.0"),
			Mask: net.CIDRMask(0, hetzner.IPv4CIDRBits),
		}

		assert.NotNil(t, ipNet.IP)
		assert.NotNil(t, ipNet.Mask)
		assert.Equal(t, "0.0.0.0/0", ipNet.String())
	})

	t.Run("IPv6AnyAddress", func(t *testing.T) {
		t.Parallel()

		// Test that IPv6 ::/0 can be created with our constants
		ipNet := net.IPNet{
			IP:   net.ParseIP("::"),
			Mask: net.CIDRMask(0, hetzner.IPv6CIDRBits),
		}

		assert.NotNil(t, ipNet.IP)
		assert.NotNil(t, ipNet.Mask)
		assert.Equal(t, "::/0", ipNet.String())
	})
}
