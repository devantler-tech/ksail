package k3shetzner_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	cloudinitbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/cloudinit"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	k3shetzner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/k3shetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testVersion     = "v1.36.1+k3s1"
	testClusterName = "test-cluster"
)

// errBoom is a sentinel used to assert error propagation from the provider.
var errBoom = errors.New("boom")

// fakeInfra is a test double for the Hetzner provider subset the provisioner uses.
type fakeInfra struct {
	nodesExist         bool
	nodesExistErr      error
	networkExists      bool
	networkExistsErr   error
	ensureNetworkErr   error
	ensureFirewallErr  error
	ensurePlacementErr error
	getSSHKeyErr       error
	deleteNodesErr     error
	startErr           error
	stopErr            error
	listResult         []string
	listErr            error

	ensureNetworkCalls int
	deleteNodesCalls   int
	startCalls         int
	stopCalls          int
	lastClusterName    string
}

func (f *fakeInfra) EnsureNetwork(
	_ context.Context,
	clusterName, _ string,
) (*hcloud.Network, error) {
	f.ensureNetworkCalls++
	f.lastClusterName = clusterName

	return &hcloud.Network{}, f.ensureNetworkErr
}

func (f *fakeInfra) EnsureFirewall(
	_ context.Context,
	_ string,
	_ []string,
) (*hcloud.Firewall, error) {
	return &hcloud.Firewall{}, f.ensureFirewallErr
}

func (f *fakeInfra) EnsurePlacementGroup(
	_ context.Context,
	_, _, _ string,
) (*hcloud.PlacementGroup, error) {
	return &hcloud.PlacementGroup{}, f.ensurePlacementErr
}

func (f *fakeInfra) GetSSHKey(_ context.Context, _ string) (*hcloud.SSHKey, error) {
	return &hcloud.SSHKey{}, f.getSSHKeyErr
}

func (f *fakeInfra) NodesExist(_ context.Context, clusterName string) (bool, error) {
	f.lastClusterName = clusterName

	return f.nodesExist, f.nodesExistErr
}

func (f *fakeInfra) NetworkExists(_ context.Context, _ string) (bool, error) {
	return f.networkExists, f.networkExistsErr
}

func (f *fakeInfra) DeleteNodes(_ context.Context, _ string) error {
	f.deleteNodesCalls++

	return f.deleteNodesErr
}

func (f *fakeInfra) StartNodes(_ context.Context, clusterName string) error {
	f.startCalls++
	f.lastClusterName = clusterName

	return f.startErr
}

func (f *fakeInfra) StopNodes(_ context.Context, clusterName string) error {
	f.stopCalls++
	f.lastClusterName = clusterName

	return f.stopErr
}

func (f *fakeInfra) ListAllClusters(_ context.Context) ([]string, error) {
	return f.listResult, f.listErr
}

// staticFakeInfraCheck asserts the test double satisfies the injected interface.
var _ k3shetzner.HetznerInfra = (*fakeInfra)(nil)

func newProvisioner(
	infra k3shetzner.HetznerInfra,
	controlPlanes, agents int,
) *k3shetzner.Provisioner {
	return k3shetzner.NewProvisionerForTest(
		infra,
		cloudinitbootstrap.New(),
		testClusterName,
		testVersion,
		controlPlanes,
		agents,
		v1alpha1.OptionsHetzner{},
		io.Discard,
	)
}

func TestBuildNodeUserDataSingleControlPlane(t *testing.T) {
	t.Parallel()

	prov := newProvisioner(&fakeInfra{}, 1, 0)

	nodes, err := prov.BuildNodeUserData(testClusterName, "token", "", nil)
	require.NoError(t, err)
	require.Len(t, nodes, 1)

	assert.Equal(t, 0, nodes[0].Index)
	assert.Equal(t, "server-init", nodes[0].Role)
	assert.True(t, strings.HasPrefix(nodes[0].UserData, "#cloud-config"))
	assert.Equal(t, "controlplane", nodes[0].Labels[hetzner.LabelNodeType])
	assert.Equal(t, "0", nodes[0].Labels[hetzner.LabelNodeIndex])
	assert.Equal(t, testClusterName, nodes[0].Labels[hetzner.LabelClusterName])
}

func TestBuildNodeUserDataMultiNodeOrderAndLabels(t *testing.T) {
	t.Parallel()

	prov := newProvisioner(&fakeInfra{}, 3, 2)

	nodes, err := prov.BuildNodeUserData(testClusterName, "token", "https://10.0.0.2:6443", nil)
	require.NoError(t, err)
	require.Len(t, nodes, 5)

	wantRoles := []string{"server-init", "server", "server", "agent", "agent"}

	for i, node := range nodes {
		assert.Equal(t, i, node.Index)
		assert.Equal(t, wantRoles[i], node.Role)
		assert.True(t, strings.HasPrefix(node.UserData, "#cloud-config"))
	}

	assert.Equal(t, "controlplane", nodes[0].Labels[hetzner.LabelNodeType])
	assert.Equal(t, "worker", nodes[4].Labels[hetzner.LabelNodeType])
}

func TestBuildNodeUserDataDeliversSSHAuthorizedKeys(t *testing.T) {
	t.Parallel()

	prov := newProvisioner(&fakeInfra{}, 1, 0)
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA ksail-bootstrap"

	nodes, err := prov.BuildNodeUserData(testClusterName, "token", "", []string{key})
	require.NoError(t, err)
	require.Len(t, nodes, 1)

	assert.Contains(t, nodes[0].UserData, "ssh_authorized_keys:")
	assert.Contains(t, nodes[0].UserData, key)
}

func TestBuildNodeUserDataErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		controlPlanes int
		agents        int
		version       string
		serverURL     string
	}{
		"joiners require a server URL": {
			controlPlanes: 2,
			agents:        0,
			version:       testVersion,
			serverURL:     "",
		},
		"missing version": {controlPlanes: 1, agents: 0, version: "", serverURL: ""},
	}

	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			prov := k3shetzner.NewProvisionerForTest(
				&fakeInfra{},
				cloudinitbootstrap.New(),
				testClusterName,
				testCase.version,
				testCase.controlPlanes,
				testCase.agents,
				v1alpha1.OptionsHetzner{},
				io.Discard,
			)

			_, err := prov.BuildNodeUserData(testClusterName, "token", testCase.serverURL, nil)
			require.Error(t, err)
		})
	}
}

func TestCreateReturnsLiveBringUpBoundary(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{}
	prov := newProvisioner(infra, 1, 0)

	err := prov.Create(context.Background(), "")
	require.ErrorIs(t, err, k3shetzner.ErrLiveBringUpNotImplemented)
	assert.Equal(t, 1, infra.ensureNetworkCalls)
	assert.Equal(t, testClusterName, infra.lastClusterName)
}

func TestCreateAlreadyExists(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{nodesExist: true}
	prov := newProvisioner(infra, 1, 0)

	err := prov.Create(context.Background(), "named")
	require.ErrorIs(t, err, k3shetzner.ErrClusterAlreadyExists)
	assert.Equal(t, 0, infra.ensureNetworkCalls)
}

func TestCreateMultiNodeNotImplemented(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{}
	prov := newProvisioner(infra, 3, 0)

	err := prov.Create(context.Background(), "")
	require.ErrorIs(t, err, k3shetzner.ErrMultiNodeNotImplemented)
	assert.Equal(t, 0, infra.ensureNetworkCalls)
}

func TestCreateNodesExistError(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{nodesExistErr: errBoom}
	prov := newProvisioner(infra, 1, 0)

	err := prov.Create(context.Background(), "")
	require.ErrorIs(t, err, errBoom)
}

func TestDeleteNetworkMissingIsNoOp(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{networkExists: false}
	prov := newProvisioner(infra, 1, 0)

	err := prov.Delete(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, 0, infra.deleteNodesCalls)
}

func TestDeleteRemovesNodes(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{networkExists: true}
	prov := newProvisioner(infra, 1, 0)

	err := prov.Delete(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, 1, infra.deleteNodesCalls)
}

func TestStartStopDelegate(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{}
	prov := newProvisioner(infra, 1, 0)

	require.NoError(t, prov.Start(context.Background(), ""))
	require.NoError(t, prov.Stop(context.Background(), ""))
	assert.Equal(t, 1, infra.startCalls)
	assert.Equal(t, 1, infra.stopCalls)
}

func TestStartPropagatesError(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{startErr: errBoom}
	prov := newProvisioner(infra, 1, 0)

	require.ErrorIs(t, prov.Start(context.Background(), ""), errBoom)
}

func TestListReturnsClusters(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{listResult: []string{"a", "b"}}
	prov := newProvisioner(infra, 1, 0)

	clusters, err := prov.List(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, clusters)
}

func TestExistsDelegates(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{nodesExist: true}
	prov := newProvisioner(infra, 1, 0)

	exists, err := prov.Exists(context.Background(), "")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestGenerateNodeTokenIsRandomHex(t *testing.T) {
	t.Parallel()

	first, err := k3shetzner.GenerateNodeToken()
	require.NoError(t, err)

	second, err := k3shetzner.GenerateNodeToken()
	require.NoError(t, err)

	assert.Len(t, first, 64)
	assert.NotEqual(t, first, second)
}

func TestCreateInfrastructureErrorsPropagate(t *testing.T) {
	t.Parallel()

	tests := map[string]*fakeInfra{
		"network":   {ensureNetworkErr: errBoom},
		"firewall":  {ensureFirewallErr: errBoom},
		"placement": {ensurePlacementErr: errBoom},
		"ssh key":   {getSSHKeyErr: errBoom},
	}

	for name, infra := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			prov := k3shetzner.NewProvisionerForTest(
				infra,
				cloudinitbootstrap.New(),
				testClusterName,
				testVersion,
				1,
				0,
				v1alpha1.OptionsHetzner{SSHKeyName: "my-key"},
				io.Discard,
			)

			err := prov.Create(context.Background(), "")
			require.ErrorIs(t, err, errBoom)
		})
	}
}

func TestDeleteNetworkCheckError(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{networkExistsErr: errBoom}
	prov := newProvisioner(infra, 1, 0)

	require.ErrorIs(t, prov.Delete(context.Background(), ""), errBoom)
}

func TestDeleteNodesError(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{networkExists: true, deleteNodesErr: errBoom}
	prov := newProvisioner(infra, 1, 0)

	require.ErrorIs(t, prov.Delete(context.Background(), ""), errBoom)
}

func TestStopPropagatesError(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{stopErr: errBoom}
	prov := newProvisioner(infra, 1, 0)

	require.ErrorIs(t, prov.Stop(context.Background(), ""), errBoom)
}

func TestListPropagatesError(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{listErr: errBoom}
	prov := newProvisioner(infra, 1, 0)

	_, err := prov.List(context.Background())
	require.ErrorIs(t, err, errBoom)
}

func TestExistsPropagatesError(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{nodesExistErr: errBoom}
	prov := newProvisioner(infra, 1, 0)

	_, err := prov.Exists(context.Background(), "")
	require.ErrorIs(t, err, errBoom)
}

func TestNewProvisionerMissingToken(t *testing.T) {
	t.Parallel()

	_, err := k3shetzner.NewProvisioner(
		testClusterName,
		testVersion,
		1,
		0,
		//nolint:gosec // G101 false positive: this is an env-var NAME, not a credential.
		v1alpha1.OptionsHetzner{TokenEnvVar: "KSAIL_K3SHETZNER_TEST_UNSET_TOKEN"},
	)
	require.Error(t, err)
}

func TestNewProvisionerSucceeds(t *testing.T) {
	t.Setenv("KSAIL_K3SHETZNER_TEST_TOKEN", "dummy-token")

	prov, err := k3shetzner.NewProvisioner(
		testClusterName,
		testVersion,
		1,
		0,
		//nolint:gosec // G101 false positive: this is an env-var NAME, not a credential.
		v1alpha1.OptionsHetzner{TokenEnvVar: "KSAIL_K3SHETZNER_TEST_TOKEN"},
	)
	require.NoError(t, err)
	assert.NotNil(t, prov)
}
