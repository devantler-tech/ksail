package kubeadmhetzner_test

import (
	"encoding/base64"
	"fmt"
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

// TestComposeInitNodeKeepsPKIOutOfUserData pins the init compose contract of the
// kubeadm two-phase flow: exactly the cluster-initialising control plane
// (bootstrap index 0) is composed regardless of the agent count, its cloud-init
// carries no cluster PKI material, its certificate SAN list carries the stable
// join name, and it runs `kubeadm init` — never a join. kubeadm must mint the PKI
// on the node because provider user-data is not a private-key transport.
func TestComposeInitNodeKeepsPKIOutOfUserData(t *testing.T) {
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

	for _, pkiPath := range []string{
		"/etc/kubernetes/pki/ca.crt",
		"/etc/kubernetes/pki/ca.key",
		"/etc/kubernetes/pki/front-proxy-ca.crt",
		"/etc/kubernetes/pki/front-proxy-ca.key",
		"/etc/kubernetes/pki/etcd/ca.crt",
		"/etc/kubernetes/pki/etcd/ca.key",
		"/etc/kubernetes/pki/sa.key",
		"/etc/kubernetes/pki/sa.pub",
	} {
		assert.NotContains(t, spec.UserData, pkiPath)
	}

	assert.NotContains(t, spec.UserData, "BEGIN CERTIFICATE")
	assert.NotContains(t, spec.UserData, "BEGIN RSA PRIVATE KEY")
}

// adminKubeconfig returns a kubeadm-shaped admin.conf with a generated CA and
// the exact discovery hash joining nodes must pin.
func adminKubeconfig(t *testing.T) ([]byte, string) {
	t.Helper()

	clusterCA, err := kubeadmhetzner.GenerateClusterCA()
	require.NoError(t, err)

	encodedCA := base64.StdEncoding.EncodeToString(clusterCA.CertPEM)
	kubeconfig := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- cluster:
    certificate-authority-data: %s
    server: https://127.0.0.1:6443
  name: kubernetes
contexts:
- context:
    cluster: kubernetes
    user: kubernetes-admin
  name: kubernetes-admin@kubernetes
current-context: kubernetes-admin@kubernetes
users:
- name: kubernetes-admin
  user: {}
`, encodedCA)

	return []byte(kubeconfig), clusterCA.DiscoveryHash
}

// TestComposeJoiningNodesPinsJoinNameAndCA pins the join compose contract:
// only the joining nodes come back (global bootstrap indices preserved, the
// init node at 0 dropped), each dials the stable join name — pinned to the
// resolved private address in /etc/hosts BEFORE `kubeadm join` runs — and each
// pins the kubeadm-minted CA's sha256 discovery hash instead of skipping CA
// verification.
func TestComposeJoiningNodesPinsJoinNameAndCA(t *testing.T) {
	t.Parallel()

	prov := newProvisioner(&fakeInfra{}, 1, 2)

	_, err := prov.ComposeInitNode(
		testClusterName, "abcdef.0123456789abcdef", composeBootstrapMaterial(),
	)
	require.NoError(t, err)

	initKubeconfig, wantDiscoveryHash := adminKubeconfig(t)

	specs, err := prov.ComposeJoiningNodes(
		testClusterName, "abcdef.0123456789abcdef",
		net.ParseIP("10.0.1.5"), initKubeconfig, composeBootstrapMaterial(),
	)
	require.NoError(t, err)
	require.Len(t, specs, 2)

	hostsPin := "echo '10.0.1.5 " + testJoinName + "' >> /etc/hosts"

	for index, spec := range specs {
		assert.Equal(t, index+1, spec.Index)
		assert.Equal(t, hetzner.NodeTypeWorker, spec.NodeType)
		assert.Contains(t, spec.UserData, testJoinName+":6443")
		assert.Contains(t, spec.UserData, wantDiscoveryHash)
		assert.NotContains(t, spec.UserData, "unsafeSkipCAVerification")
		assert.Contains(t, spec.UserData, hostsPin)

		pinAt := strings.Index(spec.UserData, hostsPin)
		joinAt := strings.Index(spec.UserData, "kubeadm join")
		require.NotEqual(t, -1, joinAt)
		assert.Less(t, pinAt, joinAt, "the /etc/hosts pin must precede `kubeadm join`")

		// The private PKI material stays on the init control plane; a worker
		// carrying any of it would leak the cluster identity.
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
		net.ParseIP("10.0.1.5"), nil, composeBootstrapMaterial(),
	)
	require.ErrorIs(t, err, kubeadmhetzner.ErrHAControlPlaneNotImplemented)
	assert.Nil(t, specs)
}

// TestComposeJoiningNodesRequiresAdminKubeconfig pins that join composition
// fails closed when the initial control plane did not yield a usable public CA.
func TestComposeJoiningNodesRequiresAdminKubeconfig(t *testing.T) {
	t.Parallel()

	prov := newProvisioner(&fakeInfra{}, 1, 2)

	_, err := prov.ComposeJoiningNodes(
		testClusterName, "abcdef.0123456789abcdef",
		net.ParseIP("10.0.1.5"), nil, composeBootstrapMaterial(),
	)
	require.ErrorIs(t, err, kubeadmhetzner.ErrInvalidAdminKubeconfig)
}
