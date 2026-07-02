package hetznerbase_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testClusterName = "test-cluster"

// errBoom is a sentinel used to assert error propagation from the provider seam.
var errBoom = errors.New("boom")

// fakeInfra is a test double for the Hetzner provider subset the base uses.
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
	createServerErr    error
	createdServer      *hcloud.Server

	networkID        int64
	firewallID       int64
	placementGroupID int64
	sshKeyID         int64

	ensureNetworkCalls int
	deleteNodesCalls   int
	startCalls         int
	stopCalls          int
	createServerCalls  int
	lastServerOpts     hetzner.CreateServerOpts
	lastClusterName    string
}

func (f *fakeInfra) EnsureNetwork(
	_ context.Context,
	clusterName, _ string,
) (*hcloud.Network, error) {
	f.ensureNetworkCalls++
	f.lastClusterName = clusterName

	return &hcloud.Network{ID: f.networkID}, f.ensureNetworkErr
}

func (f *fakeInfra) EnsureFirewall(
	_ context.Context,
	_ string,
	_ []string,
) (*hcloud.Firewall, error) {
	return &hcloud.Firewall{ID: f.firewallID}, f.ensureFirewallErr
}

func (f *fakeInfra) EnsurePlacementGroup(
	_ context.Context,
	_, _, _ string,
) (*hcloud.PlacementGroup, error) {
	return &hcloud.PlacementGroup{ID: f.placementGroupID}, f.ensurePlacementErr
}

func (f *fakeInfra) GetSSHKey(_ context.Context, _ string) (*hcloud.SSHKey, error) {
	return &hcloud.SSHKey{ID: f.sshKeyID}, f.getSSHKeyErr
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

func (f *fakeInfra) CreateServer(
	_ context.Context,
	opts hetzner.CreateServerOpts,
) (*hcloud.Server, error) {
	f.createServerCalls++
	f.lastServerOpts = opts

	return f.createdServer, f.createServerErr
}

// staticFakeInfraCheck asserts the test double satisfies the shared seams.
var (
	_ hetznerbase.Infra         = (*fakeInfra)(nil)
	_ hetznerbase.ServerCreator = (*fakeInfra)(nil)
)

func newBase(infra *fakeInfra, opts v1alpha1.OptionsHetzner) *hetznerbase.Base {
	return &hetznerbase.Base{
		Infra:         infra,
		Servers:       infra,
		Opts:          opts,
		ClusterName:   testClusterName,
		ControlPlanes: 1,
		Agents:        0,
		LogWriter:     io.Discard,
	}
}

func TestResolveName(t *testing.T) {
	t.Parallel()

	base := newBase(&fakeInfra{}, v1alpha1.OptionsHetzner{})

	assert.Equal(t, testClusterName, base.ResolveName(""))
	assert.Equal(t, "explicit", base.ResolveName("explicit"))
}

func TestEnsureInfrastructureUsesResolvedNameAndSSHKey(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{}
	base := newBase(infra, v1alpha1.OptionsHetzner{SSHKeyName: "my-key"})

	_, err := base.EnsureInfrastructure(context.Background(), testClusterName)
	require.NoError(t, err)
	assert.Equal(t, 1, infra.ensureNetworkCalls)
	assert.Equal(t, testClusterName, infra.lastClusterName)
}

func TestEnsureInfrastructureReturnsResolvedIDs(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{
		networkID:        11,
		firewallID:       22,
		placementGroupID: 33,
		sshKeyID:         44,
	}
	base := newBase(infra, v1alpha1.OptionsHetzner{SSHKeyName: "my-key"})

	resolved, err := base.EnsureInfrastructure(context.Background(), testClusterName)
	require.NoError(t, err)
	assert.Equal(t, hetznerbase.ResolvedInfra{
		NetworkID:        11,
		FirewallID:       22,
		PlacementGroupID: 33,
		SSHKeyID:         44,
	}, resolved)
}

func TestEnsureInfrastructureSkipsUnconfiguredSSHKey(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{sshKeyID: 44}
	base := newBase(infra, v1alpha1.OptionsHetzner{})

	resolved, err := base.EnsureInfrastructure(context.Background(), testClusterName)
	require.NoError(t, err)
	assert.Zero(t, resolved.SSHKeyID)
}

func TestEnsureInfrastructureErrorsPropagate(t *testing.T) {
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

			base := newBase(infra, v1alpha1.OptionsHetzner{SSHKeyName: "my-key"})

			_, err := base.EnsureInfrastructure(context.Background(), testClusterName)
			require.ErrorIs(t, err, errBoom)
		})
	}
}

func TestDeleteNetworkMissingIsNoOp(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{networkExists: false}
	base := newBase(infra, v1alpha1.OptionsHetzner{})

	require.NoError(t, base.Delete(context.Background(), ""))
	assert.Equal(t, 0, infra.deleteNodesCalls)
}

func TestDeleteRemovesNodes(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{networkExists: true}
	base := newBase(infra, v1alpha1.OptionsHetzner{})

	require.NoError(t, base.Delete(context.Background(), ""))
	assert.Equal(t, 1, infra.deleteNodesCalls)
}

func TestDeleteErrorsPropagate(t *testing.T) {
	t.Parallel()

	tests := map[string]*fakeInfra{
		"network check": {networkExistsErr: errBoom},
		"delete nodes":  {networkExists: true, deleteNodesErr: errBoom},
	}

	for name, infra := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			base := newBase(infra, v1alpha1.OptionsHetzner{})

			require.ErrorIs(t, base.Delete(context.Background(), ""), errBoom)
		})
	}
}

func TestStartStopDelegate(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{}
	base := newBase(infra, v1alpha1.OptionsHetzner{})

	require.NoError(t, base.Start(context.Background(), ""))
	require.NoError(t, base.Stop(context.Background(), ""))
	assert.Equal(t, 1, infra.startCalls)
	assert.Equal(t, 1, infra.stopCalls)
	assert.Equal(t, testClusterName, infra.lastClusterName)
}

func TestStartStopPropagateErrors(t *testing.T) {
	t.Parallel()

	require.ErrorIs(
		t,
		newBase(&fakeInfra{startErr: errBoom}, v1alpha1.OptionsHetzner{}).
			Start(context.Background(), ""),
		errBoom,
	)
	require.ErrorIs(
		t,
		newBase(&fakeInfra{stopErr: errBoom}, v1alpha1.OptionsHetzner{}).
			Stop(context.Background(), ""),
		errBoom,
	)
}

func TestListReturnsClusters(t *testing.T) {
	t.Parallel()

	base := newBase(&fakeInfra{listResult: []string{"a", "b"}}, v1alpha1.OptionsHetzner{})

	clusters, err := base.List(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, clusters)
}

func TestListPropagatesError(t *testing.T) {
	t.Parallel()

	base := newBase(&fakeInfra{listErr: errBoom}, v1alpha1.OptionsHetzner{})

	_, err := base.List(context.Background())
	require.ErrorIs(t, err, errBoom)
}

func TestExistsDelegates(t *testing.T) {
	t.Parallel()

	base := newBase(&fakeInfra{nodesExist: true}, v1alpha1.OptionsHetzner{})

	exists, err := base.Exists(context.Background(), "")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestExistsPropagatesError(t *testing.T) {
	t.Parallel()

	base := newBase(&fakeInfra{nodesExistErr: errBoom}, v1alpha1.OptionsHetzner{})

	_, err := base.Exists(context.Background(), "")
	require.ErrorIs(t, err, errBoom)
}
