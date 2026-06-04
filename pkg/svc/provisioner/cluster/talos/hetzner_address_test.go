package talosprovisioner_test

import (
	"errors"
	"net"
	"testing"

	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errTestDialTimeout is a static error used to simulate a Talos-API dial failure.
var errTestDialTimeout = errors.New("dial timeout")

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

func TestDiagnoseUnreachableNode(t *testing.T) {
	t.Parallel()

	baseErr := errTestDialTimeout

	t.Run("EnrichesIPv4LessNodeFailure", func(t *testing.T) {
		t.Parallel()

		got := talosprovisioner.DiagnoseUnreachableNode(addrTestServer("", "10.0.0.9"), baseErr)

		require.ErrorIs(t, got, talosprovisioner.ErrPrivateNetworkUnreachable)
		require.ErrorIs(t, got, baseErr)
		assert.Contains(t, got.Error(), "addr-node")
	})

	t.Run("PassesThroughForPublicIPv4Node", func(t *testing.T) {
		t.Parallel()

		got := talosprovisioner.DiagnoseUnreachableNode(
			addrTestServer("203.0.113.5", "10.0.0.5"),
			baseErr,
		)

		require.Equal(t, baseErr, got)
		require.NotErrorIs(t, got, talosprovisioner.ErrPrivateNetworkUnreachable)
	})

	t.Run("PassesThroughNilError", func(t *testing.T) {
		t.Parallel()

		require.NoError(
			t,
			talosprovisioner.DiagnoseUnreachableNode(addrTestServer("", "10.0.0.9"), nil),
		)
	})

	t.Run("PassesThroughNilServer", func(t *testing.T) {
		t.Parallel()

		require.Equal(t, baseErr, talosprovisioner.DiagnoseUnreachableNode(nil, baseErr))
	})
}
