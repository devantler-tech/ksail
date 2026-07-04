package kubeadmhetzner_test

import (
	"net"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
	kubeadmhetzner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kubeadmhetzner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// composeBootstrapMaterial is minimal composition-only bootstrap material — the
// SSH signer and host-key callback feed only the live bring-up, which these
// composition tests never reach.
func composeBootstrapMaterial() hetznerbase.BootstrapMaterial {
	return hetznerbase.BootstrapMaterial{
		AuthorizedKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA ksail-bootstrap",
	}
}

// testJoinName is the stable join name derived from testClusterName; the init
// node's certificate carries it as a SAN and the joining nodes dial it.
const testJoinName = "test-cluster-api.ksail.internal"

// TestComposeInitNodeSeedsClusterIdentity pins the init compose contract of the
// kubeadm two-phase flow: exactly the cluster-initialising control plane
// (bootstrap index 0) is composed regardless of the agent count, its cloud-init
// seeds the pre-generated cluster CA at kubeadm's canonical PKI paths (fixing
// the cluster identity before boot), its certificate SAN list carries the
// stable join name, and it runs `kubeadm init` — never a join.
func TestComposeInitNodeSeedsClusterIdentity(t *testing.T) {
	t.Parallel()

	prov := newProvisioner(&fakeInfra{}, 1, 2)

	spec, err := prov.ComposeInitNode(
		testClusterName, "abcdef.0123456789abcdef", composeBootstrapMaterial(),
	)
	require.NoError(t, err)

	assert.Equal(t, 0, spec.Index)
	assert.Equal(t, hetzner.NodeTypeControlPlane, spec.NodeType)
	assert.True(t, strings.HasPrefix(spec.UserData, "#cloud-config"))
	assert.Contains(t, spec.UserData, "/etc/kubernetes/pki/ca.crt")
	assert.Contains(t, spec.UserData, "/etc/kubernetes/pki/ca.key")
	assert.Contains(t, spec.UserData, "BEGIN CERTIFICATE")
	assert.Contains(t, spec.UserData, "BEGIN RSA PRIVATE KEY")
	assert.Contains(t, spec.UserData, testJoinName)
	assert.Contains(t, spec.UserData, "kubeadm init")
	assert.NotContains(t, spec.UserData, "kubeadm join")
}

// TestComposeJoiningNodesPinsJoinNameAndCA pins the join compose contract:
// only the joining nodes come back (global bootstrap indices preserved, the
// init node at 0 dropped), each dials the stable join name — pinned to the
// resolved private address in /etc/hosts BEFORE `kubeadm join` runs — and each
// pins the pre-seeded CA's sha256 discovery hash instead of skipping CA
// verification.
func TestComposeJoiningNodesPinsJoinNameAndCA(t *testing.T) {
	t.Parallel()

	prov := newProvisioner(&fakeInfra{}, 1, 2)

	_, err := prov.ComposeInitNode(
		testClusterName, "abcdef.0123456789abcdef", composeBootstrapMaterial(),
	)
	require.NoError(t, err)

	specs, err := prov.ComposeJoiningNodes(
		testClusterName, "abcdef.0123456789abcdef",
		net.ParseIP("10.0.1.5"), composeBootstrapMaterial(),
	)
	require.NoError(t, err)
	require.Len(t, specs, 2)

	hostsPin := "echo '10.0.1.5 " + testJoinName + "' >> /etc/hosts"

	for index, spec := range specs {
		assert.Equal(t, index+1, spec.Index)
		assert.Equal(t, hetzner.NodeTypeWorker, spec.NodeType)
		assert.Contains(t, spec.UserData, testJoinName+":6443")
		assert.Contains(t, spec.UserData, "sha256:")
		assert.Contains(t, spec.UserData, hostsPin)

		pinAt := strings.Index(spec.UserData, hostsPin)
		joinAt := strings.Index(spec.UserData, "kubeadm join")
		require.NotEqual(t, -1, joinAt)
		assert.Less(t, pinAt, joinAt, "the /etc/hosts pin must precede `kubeadm join`")
	}
}

// TestComposeJoiningNodesRequiresInitFirst pins that composing joining nodes
// without a prior init compose is refused: the joiners pin the CA minted during
// the init compose, so out-of-order composition has no identity to pin.
func TestComposeJoiningNodesRequiresInitFirst(t *testing.T) {
	t.Parallel()

	prov := newProvisioner(&fakeInfra{}, 1, 2)

	_, err := prov.ComposeJoiningNodes(
		testClusterName, "abcdef.0123456789abcdef",
		net.ParseIP("10.0.1.5"), composeBootstrapMaterial(),
	)
	require.ErrorIs(t, err, kubeadmhetzner.ErrJoiningNodesComposedFirst)
}
