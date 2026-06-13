package cluster_test

import (
	"net"
	"testing"

	cluster "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
)

// serverWith builds a Hetzner server fixture with the given public IPv4, public
// IPv6, and private-network IPs. An empty string leaves the corresponding IP
// nil (the field's zero value), exercising serverHasIP's nil-guard branches.
func serverWith(publicV4, publicV6 string, privateIPs ...string) *hcloud.Server {
	server := &hcloud.Server{}

	if publicV4 != "" {
		server.PublicNet.IPv4.IP = net.ParseIP(publicV4)
	}

	if publicV6 != "" {
		server.PublicNet.IPv6.IP = net.ParseIP(publicV6)
	}

	for _, ip := range privateIPs {
		var parsed net.IP
		if ip != "" {
			parsed = net.ParseIP(ip)
		}

		server.PrivateNet = append(server.PrivateNet, hcloud.ServerPrivateNet{IP: parsed})
	}

	return server
}

// TestServerHasIP pins serverHasIP, the Hetzner cluster-ownership check that
// guards against attributing a cluster to a server that does not actually
// expose the endpoint IP. It covers the public-IPv4, public-IPv6, and
// private-network match paths (an IPv4-less control plane matches on its
// private-network IP) plus every nil-guard and no-match fall-through branch.
//
//nolint:funlen // Test function with comprehensive test cases
func TestServerHasIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		server *hcloud.Server
		ip     string
		want   bool
	}{
		{name: "nil_server", server: nil, ip: "203.0.113.5", want: false},
		{
			name:   "public_ipv4_match",
			server: serverWith("203.0.113.5", ""),
			ip:     "203.0.113.5",
			want:   true,
		},
		{
			name:   "public_ipv6_match",
			server: serverWith("", "2001:db8::1"),
			ip:     "2001:db8::1",
			want:   true,
		},
		{
			name:   "private_net_match",
			server: serverWith("", "", "10.0.0.4"),
			ip:     "10.0.0.4",
			want:   true,
		},
		{
			name:   "private_net_match_on_last",
			server: serverWith("", "", "10.0.0.4", "10.0.0.5"),
			ip:     "10.0.0.5",
			want:   true,
		},
		{
			name:   "no_ips_no_private_nets",
			server: serverWith("", ""),
			ip:     "203.0.113.5",
			want:   false,
		},
		{
			name:   "all_present_none_match",
			server: serverWith("203.0.113.5", "2001:db8::1", "10.0.0.4"),
			ip:     "198.51.100.9",
			want:   false,
		},
		{
			name:   "private_net_nil_ip_skipped",
			server: serverWith("", "", "", "10.0.0.7"),
			ip:     "203.0.113.5",
			want:   false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := cluster.ServerHasIP(testCase.server, testCase.ip)
			assert.Equal(t, testCase.want, got)
		})
	}
}
