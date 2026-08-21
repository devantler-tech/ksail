package kubeadmhetzner_test

import (
	"net"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	cloudinitbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/cloudinit"
	sshbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/ssh"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
	"github.com/stretchr/testify/require"
)

// productionBootstrapMaterial mirrors what the live bring-up assembles, host
// identity included. The composition-only fixtures elsewhere leave HostKeys nil,
// so they never exercise the one PEM private key the supported path legitimately
// ships — which is exactly the shape a guard is most likely to reject by mistake.
func productionBootstrapMaterial(t *testing.T) hetznerbase.BootstrapMaterial {
	t.Helper()

	host, err := sshbootstrap.GenerateKeyPair()
	require.NoError(t, err)

	client, err := sshbootstrap.GenerateKeyPair()
	require.NoError(t, err)

	return hetznerbase.BootstrapMaterial{
		Signer:        client.Signer,
		AuthorizedKey: client.AuthorizedKey,
		HostKeys: &cloudinitbootstrap.HostKeys{
			ED25519Private: string(host.PrivateKeyPEM),
			ED25519Public:  host.AuthorizedKey,
		},
	}
}

// TestDeriveServerSpecsAcceptsRealBringUpUserData is the end-to-end negative
// control for the provider user-data guard.
//
// The guard's own unit tests feed it hand-written fixtures, and the composition
// tests check rendered user-data with a separate test-only matcher. Neither
// combination puts REAL renderer output through the REAL guard, so a guard that
// rejected something the supported bring-up actually emits would pass both and
// break `ksail create` at the first node. This closes that gap: every node the
// supported topology composes must survive spec derivation untouched.
func TestDeriveServerSpecsAcceptsRealBringUpUserData(t *testing.T) {
	t.Parallel()

	prov := newProvisioner(&fakeInfra{}, 1, 2)
	material := productionBootstrapMaterial(t)

	initNode, err := prov.ComposeInitNode(
		testClusterName, "abcdef.0123456789abcdef", material,
	)
	require.NoError(t, err)

	initKubeconfig, _ := adminKubeconfig(t)

	joiners, err := prov.ComposeJoiningNodes(
		testClusterName, "abcdef.0123456789abcdef",
		net.ParseIP("10.0.1.5"), initKubeconfig, material,
	)
	require.NoError(t, err)

	nodes := append([]hetznerbase.NodeSpec{initNode}, joiners...)
	require.Len(t, nodes, 3, "one control plane plus two agents")

	specs, err := hetznerbase.DeriveServerSpecs(
		testClusterName,
		nodes,
		v1alpha1.OptionsHetzner{
			ControlPlaneServerType: "cx23",
			WorkerServerType:       "cx33",
			Location:               "fsn1",
		},
		hetznerbase.ResolvedInfra{
			NetworkID: 100, FirewallID: 200, PlacementGroupID: 300, SSHKeyID: 400,
		},
	)

	require.NoError(t, err, "the supported bring-up must survive the user-data guard")
	require.Len(t, specs, 3)
}
