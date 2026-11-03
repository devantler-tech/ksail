package kindprovisioner_test

import (
	"context"
	"io"
	"net/netip"
	"os"
	"testing"

	kindprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kind"
	"github.com/docker/docker/api/types/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

func TestPlanDinDNetworkProtectsEffectiveKindNetworks(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name       string
		networking v1alpha4.Networking
		want       string
		wantError  bool
	}{
		{name: "upstream defaults", want: "base=10.0.0.0/12,size=16"},
		{name: "explicit pod and service networks", networking: v1alpha4.Networking{
			PodSubnet: "10.0.0.0/16", ServiceSubnet: "10.16.0.0/16",
		}, want: "base=10.32.0.0/12,size=16"},
		{name: "dual stack", networking: v1alpha4.Networking{
			IPFamily:  v1alpha4.DualStackFamily,
			PodSubnet: "10.0.0.0/16,fd00:1::/56", ServiceSubnet: "10.16.0.0/16,fd00:2::/112",
		}, want: "base=10.32.0.0/12,size=16"},
		{name: "IPv6 defaults", networking: v1alpha4.Networking{
			IPFamily: v1alpha4.IPv6Family,
		}, want: "base=10.0.0.0/12,size=16"},
		{name: "invalid configured prefix", networking: v1alpha4.Networking{
			PodSubnet: "not-a-prefix",
		}, wantError: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cfg := &v1alpha4.Cluster{Networking: testCase.networking}

			pool, err := kindprovisioner.PlanDinDNetworkForTest(
				context.Background(), cfg, "https://192.0.2.1:6443",
			)
			if testCase.wantError {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.want, pool)
			assert.Equal(
				t,
				testCase.networking,
				cfg.Networking,
				"defaulting must not rewrite user configuration",
			)
			assert.Empty(t, cfg.Nodes)
		})
	}
}

func TestSelectDinDNetworkExcludesKnownPrefixesAndFailsClosed(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		protected []netip.Prefix
		want      string
		wantError bool
	}{
		{
			name: "exact endpoint", protected: []netip.Prefix{netip.MustParsePrefix("10.0.1.2/32")},
			want: "base=10.16.0.0/12,size=16",
		},
		{
			name: "DNS service address", protected: []netip.Prefix{netip.MustParsePrefix("10.0.0.10/32")},
			want: "base=10.16.0.0/12,size=16",
		},
		{
			name: "broad known network", protected: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
			want: "base=172.16.0.0/12,size=16",
		},
		{name: "all candidates protected", protected: []netip.Prefix{
			netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("172.16.0.0/12"),
		}, wantError: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			pool, err := kindprovisioner.SelectDinDNetworkForTest(testCase.protected)
			if testCase.wantError {
				require.ErrorIs(t, err, kindprovisioner.ErrNoDinDAddressPool)
				assert.Empty(t, pool)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.want, pool)
		})
	}
}

func TestDinDEndpointsProtectEveryResolvedAddress(t *testing.T) {
	t.Parallel()

	addresses := []netip.Addr{
		netip.MustParseAddr("10.0.1.2"), netip.MustParseAddr("172.18.0.2"),
		netip.MustParseAddr("2001:db8::1"),
	}
	got, err := kindprovisioner.EndpointPrefixesForTest(
		context.Background(),
		"https://api.example:6443",
		func(_ context.Context, network, host string) ([]netip.Addr, error) {
			assert.Equal(t, "ip", network)
			assert.Equal(t, "api.example", host)

			return addresses, nil
		},
	)
	require.NoError(t, err)
	assert.Equal(t, []netip.Prefix{
		netip.MustParsePrefix("10.0.1.2/32"), netip.MustParsePrefix("172.18.0.2/32"),
		netip.MustParsePrefix("2001:db8::1/128"),
	}, got)

	got, err = kindprovisioner.EndpointPrefixesForTest(
		context.Background(),
		"[::ffff:10.0.1.2]:6443",
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, []netip.Prefix{netip.MustParsePrefix("10.0.1.2/32")}, got)
}

func TestDinDEndpointsRefuseUnknownAddresses(t *testing.T) {
	t.Parallel()

	lookupError := os.ErrNotExist
	for _, testCase := range []struct {
		name, endpoint string
		addresses      []netip.Addr
		lookupErr      error
	}{
		{name: "empty", endpoint: ""},
		{name: "no DNS answers", endpoint: "api.example"},
		{name: "lookup failure", endpoint: "api.example", lookupErr: lookupError},
		{name: "unspecified", endpoint: "https://0.0.0.0:6443"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := kindprovisioner.EndpointPrefixesForTest(
				context.Background(),
				testCase.endpoint,
				func(context.Context, string, string) ([]netip.Addr, error) {
					return testCase.addresses, testCase.lookupErr
				},
			)
			require.Error(t, err)
		})
	}
}

type networkReply struct {
	networks []network.Summary
	err      error
}

func (reply networkReply) NetworkList(
	context.Context,
	network.ListOptions,
) ([]network.Summary, error) {
	return reply.networks, reply.err
}

func TestDinDNetworkInspectionRefusesPersistedAllocationsOutsidePlan(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name, subnet string
		wantError    bool
	}{
		{name: "compatible", subnet: "10.16.0.0/16"},
		{name: "known endpoint collision", subnet: "10.0.0.0/16", wantError: true},
		{name: "unplanned old allocation", subnet: "192.168.0.0/16", wantError: true},
		{name: "supernet crosses pool boundary", subnet: "10.0.0.0/8", wantError: true},
		{name: "insufficient per-network capacity", subnet: "10.16.0.0/24", wantError: true},
		{name: "missing allocation", wantError: true},
		{name: "malformed allocation", subnet: "garbage", wantError: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := kindprovisioner.ValidateDinDNetworksForTest(context.Background(), networkReply{
				networks: []network.Summary{{Name: "bridge", Driver: "bridge", IPAM: network.IPAM{
					Config: []network.IPAMConfig{{Subnet: testCase.subnet}},
				}}},
			}, false)
			if testCase.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestDinDNetworkInspectionRequiresCompleteUniqueBridgeState(t *testing.T) {
	t.Parallel()
	require.Error(
		t,
		kindprovisioner.ValidateDinDNetworksForTest(context.Background(), networkReply{}, false),
	)
	require.Error(t, kindprovisioner.ValidateDinDNetworksForTest(context.Background(), networkReply{
		err: io.ErrUnexpectedEOF,
	}, false))
	require.Error(t, kindprovisioner.ValidateDinDNetworksForTest(context.Background(), networkReply{
		networks: []network.Summary{{Name: "bridge", Driver: "bridge", IPAM: network.IPAM{
			Config: []network.IPAMConfig{{Subnet: "10.16.0.0/16"}},
		}}},
	}, true), "success requires the actual Kind bridge as well")
	valid := []network.Summary{
		{
			Name:   "bridge",
			Driver: "bridge",
			IPAM:   network.IPAM{Config: []network.IPAMConfig{{Subnet: "10.16.0.0/16"}}},
		},
		{
			Name:   "kind",
			Driver: "bridge",
			IPAM: network.IPAM{
				Config: []network.IPAMConfig{{Subnet: "10.17.0.0/16"}, {Subnet: "fd00:4::/64"}},
			},
		},
	}
	require.NoError(
		t,
		kindprovisioner.ValidateDinDNetworksForTest(
			context.Background(),
			networkReply{networks: valid},
			true,
		),
	)
	require.Error(t, kindprovisioner.ValidateDinDNetworksForTest(context.Background(), networkReply{
		networks: []network.Summary{valid[0], valid[1], valid[1]},
	}, false), "refuse duplicates before Kind can delete a potentially populated duplicate")
}
