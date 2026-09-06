package talosprovisioner_test

import (
	"context"
	"io"
	"net"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoveHetznerNodes_CleanupFailurePreservesServer(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name         string
		address      string
		leaveFailure bool
	}{
		{name: "missing target address"},
		{name: "connection failure", address: "192.0.2.1"},
		{name: "leave failure", address: "192.0.2.1", leaveFailure: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			transport := &membershipCloudTransport{address: testCase.address}

			provisioner := newClientErrProvisioner(
				t,
			).WithInfraProvider(newMembershipCloud(transport))
			if testCase.leaveFailure {
				provisioner.WithEtcdClientFactoryForTest(
					func(context.Context, string) (talosprovisioner.EtcdMembershipClientForTest, error) {
						return &membershipClient{leaveErr: errMembershipUnavailable}, nil
					},
				)
			}

			result := clusterupdate.NewEmptyUpdateResult()

			err := provisioner.RemoveHetznerNodesForTest(
				t.Context(),
				"scale-cluster",
				talosprovisioner.RoleControlPlane,
				1,
				result,
			)

			require.Error(t, err)
			assert.Empty(
				t,
				transport.writes,
				"membership failure must precede every infrastructure mutation",
			)
			assert.Empty(t, result.AppliedChanges)
			assert.Len(t, result.FailedChanges, 1)

			if testCase.leaveFailure {
				assert.ErrorIs(t, err, errMembershipUnavailable)
			}
		})
	}
}

// TestWaitForNewHetznerNodesReachable_NoServersIsNoOp verifies the post-config
// reachability wait is a no-op (no dialing, no error) when there are no new nodes.
func TestWaitForNewHetznerNodesReachable_NoServersIsNoOp(t *testing.T) {
	t.Parallel()

	provisioner := talosprovisioner.NewProvisioner(nil, nil).WithLogWriter(io.Discard)

	err := provisioner.WaitForNewHetznerNodesReachableForTest(
		context.Background(), nil, talosprovisioner.RoleWorker,
	)
	require.NoError(t, err)
}

// TestWaitForNewHetznerNodesReachable_WaitsAndSurfacesFailure verifies that
// scale-up's post-config wait actually polls the new node for reachability and
// surfaces a failure (with the role in the error) rather than returning success
// without waiting. This is the guard that stops the in-place config reconciliation
// from racing a just-created node's install+reboot — the cause of the reported
// "connection refused" failure when scaling Hetzner workers.
func TestWaitForNewHetznerNodesReachable_WaitsAndSurfacesFailure(t *testing.T) {
	t.Parallel()

	provisioner := talosprovisioner.NewProvisioner(nil, nil).WithLogWriter(io.Discard)

	// RFC 5737 TEST-NET-1 address: guaranteed unroutable, so the node is never
	// reachable. The cancelled context aborts the wait promptly without a real dial.
	server := &hcloud.Server{Name: "prod-worker-4"}
	server.PublicNet.IPv4.IP = net.ParseIP("192.0.2.1")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := provisioner.WaitForNewHetznerNodesReachableForTest(
		ctx, []*hcloud.Server{server}, talosprovisioner.RoleWorker,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), talosprovisioner.RoleWorker,
		"error should name the role of the nodes that never became reachable")
}
