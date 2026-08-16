package kubeadmhetzner_test

import (
	"context"
	"errors"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
	kubeadmhetzner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kubeadmhetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"
)

// testVersion and testClusterName are declared in userdata_test.go (same test
// package) and reused here.

// tokenPattern is the kubeadm bootstrap-token format the generator must produce.
var tokenPattern = regexp.MustCompile(`^[a-z0-9]{6}\.[a-z0-9]{16}$`)

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
var _ kubeadmhetzner.HetznerInfra = (*fakeInfra)(nil)

func newProvisioner(
	infra kubeadmhetzner.HetznerInfra,
	controlPlanes, agents int,
) *kubeadmhetzner.Provisioner {
	return kubeadmhetzner.NewProvisionerForTest(
		infra,
		testClusterName,
		testVersion,
		controlPlanes,
		agents,
		v1alpha1.OptionsHetzner{},
		io.Discard,
	)
}

func TestBuildNodesSingleControlPlane(t *testing.T) {
	t.Parallel()

	prov := newProvisioner(&fakeInfra{}, 1, 0)

	token, err := kubeadmhetzner.GenerateNodeToken()
	require.NoError(t, err)

	nodes, err := prov.BuildNodes(testClusterName, token)
	require.NoError(t, err)
	require.Len(t, nodes, 1)

	assert.Equal(t, 0, nodes[0].Index)
	assert.Equal(t, "server-init", string(nodes[0].Role))
	assert.True(t, strings.HasPrefix(nodes[0].UserData, "#cloud-config"))
	assert.Equal(t, "controlplane", nodes[0].Labels[hetzner.LabelNodeType])
	assert.Equal(t, "0", nodes[0].Labels[hetzner.LabelNodeIndex])
	assert.Equal(t, testClusterName, nodes[0].Labels[hetzner.LabelClusterName])
}

func TestBuildNodesMissingVersion(t *testing.T) {
	t.Parallel()

	// The kubeadm install renderer requires the cluster-wide Kubernetes version (it
	// selects the package repository track), so composing without it fails even for
	// a single node.
	prov := kubeadmhetzner.NewProvisionerForTest(
		&fakeInfra{},
		testClusterName,
		"",
		1,
		0,
		v1alpha1.OptionsHetzner{},
		io.Discard,
	)

	_, err := prov.BuildNodes(testClusterName, "abcdef.0123456789abcdef")
	require.Error(t, err)
}

// fakeServers is a test double for the server-creation seam the bring-up
// engine uses.
type fakeServers struct {
	createErr error
	lastOpts  hetzner.CreateServerOpts
	calls     int
}

func (f *fakeServers) CreateServer(
	_ context.Context,
	opts hetzner.CreateServerOpts,
) (*hcloud.Server, error) {
	f.calls++
	f.lastOpts = opts

	return nil, f.createErr
}

// staticFakeServersCheck asserts the test double satisfies the injected seam.
var _ kubeadmhetzner.HetznerServers = (*fakeServers)(nil)

func TestComposePlanDerivesCompleteSingleNodePlan(t *testing.T) {
	t.Parallel()

	prov := kubeadmhetzner.NewProvisionerForTest(
		&fakeInfra{},
		testClusterName,
		testVersion,
		1,
		0,
		v1alpha1.OptionsHetzner{
			ControlPlaneServerType: "cx23",
			WorkerServerType:       "cx33",
			Location:               "fsn1",
		},
		io.Discard,
	)

	plan, err := prov.ComposePlan(
		testClusterName, "abcdef.0123456789abcdef",
		hetznerbase.ResolvedInfra{
			NetworkID:        100,
			FirewallID:       200,
			PlacementGroupID: 300,
			SSHKeyID:         400,
		},
	)
	require.NoError(t, err)

	// The single control-plane spec is fully derived: name, per-role server
	// type, stock boot image, and the resolved infrastructure placement.
	require.Len(t, plan.Specs, 1)
	spec := plan.Specs[0]
	assert.Equal(t, "test-cluster-controlplane-0", spec.Name)
	assert.Equal(t, "cx23", spec.ServerType)
	assert.Equal(t, hetznerbase.DefaultImageName, spec.ImageName)
	assert.Equal(t, "fsn1", spec.Location)
	assert.Equal(t, int64(100), spec.NetworkID)
	assert.Equal(t, []int64{200}, spec.FirewallIDs)
	assert.Equal(t, int64(300), spec.PlacementGroupID)
	assert.Equal(t, int64(400), spec.SSHKeyID)

	// The bootstrap public key and the pre-generated host identity are
	// delivered via cloud-init, and the plan carries their counterparts (the
	// dialing signer and the pinning host-key callback).
	require.NotNil(t, plan.Signer)

	authorizedKey := strings.TrimSpace(
		string(gossh.MarshalAuthorizedKey(plan.Signer.PublicKey())),
	)

	assert.Contains(t, spec.UserData, "ssh_authorized_keys:")
	assert.Contains(t, spec.UserData, authorizedKey)
	assert.Contains(t, spec.UserData, "ssh_keys:")
	assert.Contains(t, spec.UserData, "ed25519_private:")
	assert.NotNil(t, plan.HostKeyCallback)

	assert.Equal(t, "/etc/kubernetes/admin.conf", plan.RemoteKubeconfigPath)
}

func TestCreateServerCreationFailureSurfaces(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{}
	servers := &fakeServers{createErr: errBoom}
	prov := newProvisioner(infra, 1, 0)
	prov.Servers = servers

	err := prov.Create(context.Background(), "")
	require.ErrorIs(t, err, errBoom)
	assert.Equal(t, 1, infra.ensureNetworkCalls)
	assert.Equal(t, testClusterName, infra.lastClusterName)

	// The composed spec reached the server-creation seam fully derived; a
	// failed creation leaves nothing to clean up.
	assert.Equal(t, 1, servers.calls)
	assert.Equal(t, "test-cluster-controlplane-0", servers.lastOpts.Name)
	assert.NotEmpty(t, servers.lastOpts.UserData)
	assert.Equal(t, 0, infra.deleteNodesCalls)
}

func TestCreateAlreadyExists(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{nodesExist: true}
	prov := newProvisioner(infra, 1, 0)

	err := prov.Create(context.Background(), "named")
	require.ErrorIs(t, err, kubeadmhetzner.ErrClusterAlreadyExists)
	assert.Equal(t, 0, infra.ensureNetworkCalls)
}

// TestCreateHATopologyIsRejected pins that kubeadm Hetzner does not enable
// multi-control-plane creation until additional control-plane private PKI can
// be transferred without exposing it through provider user-data.
func TestCreateHATopologyIsRejected(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{}
	servers := &fakeServers{createErr: errBoom}
	prov := newProvisioner(infra, 3, 0)
	prov.Servers = servers

	err := prov.Create(context.Background(), "")
	require.ErrorIs(t, err, kubeadmhetzner.ErrHAControlPlaneNotImplemented)
	require.NotErrorIs(t, err, errBoom)

	assert.Equal(t, 0, infra.ensureNetworkCalls)
	assert.Equal(t, 0, servers.calls)
}

// The provisioner now satisfies hetznerbase.MultiNodeComposer (multinode.go's
// compile-time check), so a topology with agents routes to the shared two-phase
// bring-up instead of being rejected; the compose halves of that flow are pinned
// by multinode_test.go and the engine by the hetznerbase package's own tests.

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

func TestGenerateNodeTokenMatchesKubeadmFormat(t *testing.T) {
	t.Parallel()

	first, err := kubeadmhetzner.GenerateNodeToken()
	require.NoError(t, err)

	second, err := kubeadmhetzner.GenerateNodeToken()
	require.NoError(t, err)

	assert.Regexp(t, tokenPattern, first)
	assert.Regexp(t, tokenPattern, second)
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

			prov := kubeadmhetzner.NewProvisionerForTest(
				infra,
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

	_, err := kubeadmhetzner.NewProvisioner(
		testClusterName,
		"test-kubeconfig",
		testVersion,
		1,
		0,
		//nolint:gosec // G101 false positive: this is an env-var NAME, not a credential.
		v1alpha1.OptionsHetzner{TokenEnvVar: "KSAIL_KUBEADMHETZNER_TEST_UNSET_TOKEN"},
	)
	require.Error(t, err)
}

func TestNewProvisionerSucceeds(t *testing.T) {
	t.Setenv("KSAIL_KUBEADMHETZNER_TEST_TOKEN", "dummy-token")

	prov, err := kubeadmhetzner.NewProvisioner(
		testClusterName,
		"test-kubeconfig",
		testVersion,
		1,
		0,
		//nolint:gosec // G101 false positive: this is an env-var NAME, not a credential.
		v1alpha1.OptionsHetzner{TokenEnvVar: "KSAIL_KUBEADMHETZNER_TEST_TOKEN"},
	)
	require.NoError(t, err)
	assert.NotNil(t, prov)
}
