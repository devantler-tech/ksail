package talosprovisioner_test

import (
	"net"
	"testing"

	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// addrTestServer builds an *hcloud.Server with the given public IPv4 and/or
// private-network IP (empty string omits that address).
func addrTestServer(publicIPv4, privateIP string) *hcloud.Server {
	server := &hcloud.Server{Name: "addr-node"}

	if publicIPv4 != "" {
		server.PublicNet.IPv4.IP = net.ParseIP(publicIPv4)
	}

	if privateIP != "" {
		server.PrivateNet = []hcloud.ServerPrivateNet{{IP: net.ParseIP(privateIP)}}
	}

	return server
}

func TestHetznerNodeTalosAddress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		server  *hcloud.Server
		want    string
		wantErr bool
	}{
		{
			name:   "PrefersPublicIPv4",
			server: addrTestServer("203.0.113.5", "10.0.0.5"),
			want:   "203.0.113.5",
		},
		{name: "FallsBackToPrivateIP", server: addrTestServer("", "10.0.0.9"), want: "10.0.0.9"},
		{name: "ErrorsWhenNoAddress", server: addrTestServer("", ""), wantErr: true},
		{name: "ErrorsOnNilServer", server: nil, wantErr: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := talosprovisioner.HetznerNodeTalosAddress(testCase.server)
			if testCase.wantErr {
				require.ErrorIs(t, err, talosprovisioner.ErrNodeNoReachableAddress)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.want, got)
		})
	}
}
