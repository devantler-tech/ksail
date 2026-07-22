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
	assert.Contains(t, spec.UserData, testJoinName)
	assert.Contains(t, spec.UserData, "kubeadm init")
	assert.NotContains(t, spec.UserData, "kubeadm join")

	// The full shared PKI — not just the cluster CA — lands at kubeadm's
	// canonical paths, so the whole cluster identity is fixed before boot and a
	// later HA increment can seed the same material onto additional control
	// planes (kubeadm's manual certificate distribution). Each path is tied to
	// its own write_files entry's mode and PEM block type, so a field swap in
	// pkiSeedFiles (a key under a cert path, or a world-readable private key)
	// fails here rather than on the booted node.
	for _, seeded := range []struct {
		path        string
		permissions string
		pemHeader   string
	}{
		{"/etc/kubernetes/pki/ca.crt", "0644", "BEGIN CERTIFICATE"},
		{"/etc/kubernetes/pki/ca.key", "0600", "BEGIN RSA PRIVATE KEY"},
		{"/etc/kubernetes/pki/front-proxy-ca.crt", "0644", "BEGIN CERTIFICATE"},
		{"/etc/kubernetes/pki/front-proxy-ca.key", "0600", "BEGIN RSA PRIVATE KEY"},
		{"/etc/kubernetes/pki/etcd/ca.crt", "0644", "BEGIN CERTIFICATE"},
		{"/etc/kubernetes/pki/etcd/ca.key", "0600", "BEGIN RSA PRIVATE KEY"},
		{"/etc/kubernetes/pki/sa.key", "0600", "BEGIN RSA PRIVATE KEY"},
		{"/etc/kubernetes/pki/sa.pub", "0644", "BEGIN PUBLIC KEY"},
	} {
		entry := writeFilesEntry(t, spec.UserData, seeded.path)
		assert.Contains(t, entry, seeded.permissions, seeded.path)
		assert.Contains(t, entry, seeded.pemHeader, seeded.path)
	}
}

// writeFilesEntry extracts the single write_files entry for path from the
// rendered cloud-init user data: the slice from its `path:` line up to the
// next entry's `path:` line (or the document end). Field order within an
// entry is path → permissions → content, so the slice carries exactly that
// entry's mode and content.
func writeFilesEntry(t *testing.T, userData, path string) string {
	t.Helper()

	start := strings.Index(userData, "path: "+path)
	require.NotEqual(t, -1, start, "write_files entry for %s not found", path)

	entry := userData[start:]
	if next := strings.Index(entry[1:], "path: /"); next != -1 {
		entry = entry[:next+1]
	}

	return entry
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

		// The private PKI material is seeded onto the init control plane only;
		// a worker carrying any of it would leak the cluster identity.
		for _, path := range []string{
			"/etc/kubernetes/pki/ca.key",
			"/etc/kubernetes/pki/front-proxy-ca.key",
			"/etc/kubernetes/pki/etcd/ca.key",
			"/etc/kubernetes/pki/sa.key",
		} {
			assert.NotContains(t, spec.UserData, path,
				"joining nodes must never receive private PKI material")
		}
	}
}

// TestComposeInitNodeDoesNotAdvertiseControlPlaneEndpointForHA pins that the
// kubeadm Hetzner composer does not prepare the cloud-init-only HA join path.
// Re-enabling this would require a private-key transfer mechanism that does
// not expose cluster signing keys through provider user-data.
func TestComposeInitNodeDoesNotAdvertiseControlPlaneEndpointForHA(t *testing.T) {
	t.Parallel()

	haProv := newProvisioner(&fakeInfra{}, 3, 1)

	haSpec, err := haProv.ComposeInitNode(
		testClusterName, "abcdef.0123456789abcdef", composeBootstrapMaterial(),
	)
	require.NoError(t, err)

	assert.NotContains(t, haSpec.UserData, "controlPlaneEndpoint")
	assert.NotContains(t, haSpec.UserData, "127.0.0.1 "+testJoinName)

	singleProv := newProvisioner(&fakeInfra{}, 1, 2)

	singleSpec, err := singleProv.ComposeInitNode(
		testClusterName, "abcdef.0123456789abcdef", composeBootstrapMaterial(),
	)
	require.NoError(t, err)

	assert.NotContains(t, singleSpec.UserData, "controlPlaneEndpoint")
	assert.NotContains(t, singleSpec.UserData, "127.0.0.1 "+testJoinName)
}

// TestComposeJoiningNodesRejectsAdditionalControlPlanes pins that additional
// kubeadm Hetzner control planes are refused instead of being composed with
// private cluster PKI in cloud-init user-data.
func TestComposeJoiningNodesRejectsAdditionalControlPlanes(t *testing.T) {
	t.Parallel()

	prov := newProvisioner(&fakeInfra{}, 3, 1)

	_, err := prov.ComposeInitNode(
		testClusterName, "abcdef.0123456789abcdef", composeBootstrapMaterial(),
	)
	require.NoError(t, err)

	specs, err := prov.ComposeJoiningNodes(
		testClusterName, "abcdef.0123456789abcdef",
		net.ParseIP("10.0.1.5"), composeBootstrapMaterial(),
	)
	require.ErrorIs(t, err, kubeadmhetzner.ErrHAControlPlaneNotImplemented)
	assert.Nil(t, specs)
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
