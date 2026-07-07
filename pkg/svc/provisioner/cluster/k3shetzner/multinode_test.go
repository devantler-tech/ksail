package k3shetzner_test

import (
	"net"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testBootstrapMaterial is minimal composition-only bootstrap material: only the
// authorized key and host identity feed node composition (the SSH signer/host-key
// callback matter only to the live bring-up, which these composition tests do not
// exercise).
func testBootstrapMaterial() hetznerbase.BootstrapMaterial {
	return hetznerbase.BootstrapMaterial{
		AuthorizedKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA ksail-bootstrap",
	}
}

// TestComposeInitNodeIsSingleControlPlane pins that ComposeInitNode composes
// exactly the cluster-initialising control plane (bootstrap index 0), regardless
// of the configured agent count — the joining nodes are composed separately once
// the control plane's address is known.
func TestComposeInitNodeIsSingleControlPlane(t *testing.T) {
	t.Parallel()

	prov := newProvisioner(&fakeInfra{}, 1, 2)

	spec, err := prov.ComposeInitNode(testClusterName, "token", testBootstrapMaterial())
	require.NoError(t, err)

	assert.Equal(t, 0, spec.Index)
	assert.Equal(t, hetzner.NodeTypeControlPlane, spec.NodeType)
	assert.True(t, strings.HasPrefix(spec.UserData, "#cloud-config"))
	// The init node initialises the cluster, so it carries no registration URL.
	assert.NotContains(t, spec.UserData, "K3S_URL")
}

// TestComposeJoiningNodesThreadsPrivateJoinURL pins that ComposeJoiningNodes
// returns only the joining nodes (the init control plane at index 0 dropped),
// with their global bootstrap indices preserved and the derived private-network
// registration URL (https://<join-address>:6443) threaded into every agent's
// cloud-init.
func TestComposeJoiningNodesThreadsPrivateJoinURL(t *testing.T) {
	t.Parallel()

	prov := newProvisioner(&fakeInfra{}, 1, 2)

	specs, err := prov.ComposeJoiningNodes(
		testClusterName, "token", net.ParseIP("10.0.1.5"), testBootstrapMaterial(),
	)
	require.NoError(t, err)
	require.Len(t, specs, 2)

	for index, spec := range specs {
		assert.Equal(t, index+1, spec.Index)
		assert.Equal(t, hetzner.NodeTypeWorker, spec.NodeType)
		assert.Contains(t, spec.UserData, "https://10.0.1.5:6443")
	}
}

// TestComposeJoiningNodesIncludesAdditionalControlPlanes pins the HA topology:
// with more than one control plane, ComposeJoiningNodes returns the additional
// control-plane servers first (tagged control planes, joining the init server via
// --server and publishing the readiness-gated join-complete sentinel), then the
// agents (tagged workers, no sentinel) — the shape the shared flow serialises.
func TestComposeJoiningNodesIncludesAdditionalControlPlanes(t *testing.T) {
	t.Parallel()

	// 3 control planes + 1 agent → after the init node, 2 additional control
	// planes then 1 agent.
	prov := newProvisioner(&fakeInfra{}, 3, 1)

	specs, err := prov.ComposeJoiningNodes(
		testClusterName, "token", net.ParseIP("10.0.1.5"), testBootstrapMaterial(),
	)
	require.NoError(t, err)
	require.Len(t, specs, 3)

	// The two additional control planes come first: they join the init server's
	// embedded etcd and publish the join-complete sentinel behind the /readyz gate.
	for _, spec := range specs[:2] {
		assert.Equal(t, hetzner.NodeTypeControlPlane, spec.NodeType)
		assert.Contains(t, spec.UserData, "https://10.0.1.5:6443")
		assert.Contains(t, spec.UserData, "readyz")
		assert.Contains(t, spec.UserData, "k3s-server-join-complete")
	}

	// The agent follows: a worker that registers via K3S_URL and never writes the
	// control-plane join sentinel.
	agent := specs[2]
	assert.Equal(t, hetzner.NodeTypeWorker, agent.NodeType)
	assert.Contains(t, agent.UserData, "K3S_URL")
	assert.NotContains(t, agent.UserData, "k3s-server-join-complete")
}
