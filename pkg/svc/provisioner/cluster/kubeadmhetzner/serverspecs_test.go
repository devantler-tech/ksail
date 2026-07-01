package kubeadmhetzner_test

import (
	"strings"
	"testing"

	kubeadmbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/kubeadm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kubeadmhetzner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// twoNodeUserData is a control-plane-plus-worker pair mirroring what
// BuildNodeUserData emits: a cluster-initialising control plane at index 0 and a
// worker at index 1, each carrying its cloud-init document and Hetzner labels.
func twoNodeUserData() []kubeadmhetzner.NodeUserData {
	return []kubeadmhetzner.NodeUserData{
		{
			Index:    0,
			Role:     kubeadmbootstrap.RoleServerInit,
			UserData: "#cloud-config\n# control plane",
			Labels: hetzner.NodeLabels(
				testClusterName, hetzner.NodeTypeControlPlane, 0,
			),
		},
		{
			Index:    1,
			Role:     kubeadmbootstrap.RoleAgent,
			UserData: "#cloud-config\n# worker",
			Labels: hetzner.NodeLabels(
				testClusterName, hetzner.NodeTypeWorker, 1,
			),
		},
	}
}

func testInfra() kubeadmhetzner.Infra {
	enableIPv4, enableIPv6 := true, false

	return kubeadmhetzner.Infra{
		ServerType:       "cx22",
		Location:         "fsn1",
		ImageID:          114690387,
		NetworkID:        100,
		FirewallID:       200,
		PlacementGroupID: 300,
		SSHKeyID:         400,
		EnableIPv4:       &enableIPv4,
		EnableIPv6:       &enableIPv6,
	}
}

func TestDeriveServerSpecs(t *testing.T) {
	t.Parallel()

	nodes := twoNodeUserData()
	infra := testInfra()

	specs, err := kubeadmhetzner.DeriveServerSpecs(testClusterName, nodes, infra)

	require.NoError(t, err)
	require.Len(t, specs, len(nodes))

	// Control plane (index 0): name and labels agree; cloud-init and infra placement
	// carried through verbatim.
	controlPlane := specs[0]
	assert.Equal(t, "test-cluster-controlplane-0", controlPlane.Name)
	assert.Equal(t, nodes[0].UserData, controlPlane.UserData)
	assert.Equal(t, nodes[0].Labels, controlPlane.Labels)
	assert.Equal(t, "cx22", controlPlane.ServerType)
	assert.Equal(t, "fsn1", controlPlane.Location)
	assert.Equal(t, int64(114690387), controlPlane.ImageID)
	assert.Equal(t, int64(100), controlPlane.NetworkID)
	assert.Equal(t, int64(300), controlPlane.PlacementGroupID)
	assert.Equal(t, int64(400), controlPlane.SSHKeyID)
	assert.Equal(t, []int64{200}, controlPlane.FirewallIDs)
	require.NotNil(t, controlPlane.EnableIPv4)
	assert.True(t, *controlPlane.EnableIPv4)
	require.NotNil(t, controlPlane.EnableIPv6)
	assert.False(t, *controlPlane.EnableIPv6)

	// Worker (index 1): the global bootstrap index is reused for the name, matching
	// the ksail.node.index label.
	worker := specs[1]
	assert.Equal(t, "test-cluster-worker-1", worker.Name)
	assert.Equal(t, nodes[1].UserData, worker.UserData)
	assert.Equal(t, nodes[1].Labels, worker.Labels)
	assert.Equal(t, "1", worker.Labels[hetzner.LabelNodeIndex])
}

func TestDeriveServerSpecsNoFirewall(t *testing.T) {
	t.Parallel()

	infra := testInfra()
	infra.FirewallID = 0

	specs, err := kubeadmhetzner.DeriveServerSpecs(testClusterName, twoNodeUserData(), infra)

	require.NoError(t, err)
	require.NotEmpty(t, specs)
	// A zero firewall ID attaches no firewall rather than a bogus firewall 0.
	assert.Nil(t, specs[0].FirewallIDs)
}

func TestDeriveServerSpecsEmpty(t *testing.T) {
	t.Parallel()

	specs, err := kubeadmhetzner.DeriveServerSpecs(
		testClusterName, []kubeadmhetzner.NodeUserData{}, testInfra(),
	)

	require.NoError(t, err)
	assert.Empty(t, specs)
}

func TestDeriveServerSpecsNameTooLong(t *testing.T) {
	t.Parallel()

	// A cluster name at the 63-char cap plus the "-controlplane-0" suffix exceeds
	// the DNS-1123 label limit, so the whole derivation fails (no partial result).
	longName := strings.Repeat("a", hetzner.MaxNodeNameLength)
	nodes := []kubeadmhetzner.NodeUserData{
		{Index: 0, Role: kubeadmbootstrap.RoleServerInit},
	}

	specs, err := kubeadmhetzner.DeriveServerSpecs(longName, nodes, testInfra())

	require.Error(t, err)
	require.ErrorIs(t, err, hetzner.ErrNodeNameTooLong)
	assert.Nil(t, specs)
}

// TestDeriveServerSpecsFromBuildNodeUserData wires the two composers end to end:
// BuildNodeUserData produces the nodes, DeriveServerSpecs turns them into server
// specs — the real path the provisioner will follow.
func TestDeriveServerSpecsFromBuildNodeUserData(t *testing.T) {
	t.Parallel()

	nodes, err := kubeadmhetzner.BuildNodeUserData(singleControlPlaneInput())
	require.NoError(t, err)
	require.Len(t, nodes, 1)

	specs, err := kubeadmhetzner.DeriveServerSpecs(testClusterName, nodes, testInfra())

	require.NoError(t, err)
	require.Len(t, specs, 1)
	assert.Equal(t, "test-cluster-controlplane-0", specs[0].Name)
	// The composed cloud-init document flows through unchanged onto the server.
	assert.Equal(t, nodes[0].UserData, specs[0].UserData)
	assert.True(t, strings.HasPrefix(specs[0].UserData, "#cloud-config"))
}
