package hetznerbase_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	sshbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/ssh"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"
)

// fakeMultiNodeStrategy is a [hetznerbase.CreateStrategy] +
// [hetznerbase.MultiNodeComposer] test double: it records the join address the
// engine derives and returns a fixed number of agent specs, so the two-phase
// engine's sequencing can be asserted without a real distribution.
type fakeMultiNodeStrategy struct {
	kubeconfigPath      string
	agents              int
	receivedJoinAddress net.IP
	composeInitErr      error
	composeJoinErr      error
}

func (f *fakeMultiNodeStrategy) ComposeNodes(
	_, _ string,
	_ hetznerbase.BootstrapMaterial,
) ([]hetznerbase.NodeSpec, error) {
	return nil, nil // unreached on the multi-node path
}

func (f *fakeMultiNodeStrategy) RemoteKubeconfigPath() string { return f.kubeconfigPath }

func (f *fakeMultiNodeStrategy) DistroLabel() string { return "Fake × Hetzner" }

func (f *fakeMultiNodeStrategy) GenerateToken() (string, error) { return "fake-token", nil }

func (f *fakeMultiNodeStrategy) ComposeInitNode(
	_, _ string,
	_ hetznerbase.BootstrapMaterial,
) (hetznerbase.NodeSpec, error) {
	if f.composeInitErr != nil {
		return hetznerbase.NodeSpec{}, f.composeInitErr
	}

	return hetznerbase.NodeSpec{
		Index:    0,
		NodeType: hetzner.NodeTypeControlPlane,
		UserData: "#cloud-config\n",
	}, nil
}

func (f *fakeMultiNodeStrategy) ComposeJoiningNodes(
	_, _ string,
	joinAddress net.IP,
	_ hetznerbase.BootstrapMaterial,
) ([]hetznerbase.NodeSpec, error) {
	f.receivedJoinAddress = joinAddress

	if f.composeJoinErr != nil {
		return nil, f.composeJoinErr
	}

	specs := make([]hetznerbase.NodeSpec, 0, f.agents)
	for index := 1; index <= f.agents; index++ {
		specs = append(specs, hetznerbase.NodeSpec{
			Index:    index,
			NodeType: hetzner.NodeTypeWorker,
			UserData: "#cloud-config\n",
		})
	}

	return specs, nil
}

// testMultiNodeAgents is the agent count the multi-node engine tests bring up
// alongside the single control plane.
const testMultiNodeAgents = 2

// testJoinPrivateIP is the control plane's private-network IPv4 the engine derives
// the agents' join address from.
const testJoinPrivateIP = "10.0.1.5"

// newMultiNodeBase wires a Base against the in-process SSH server for the control
// plane's bring-up and the fake strategy for composition. When withPrivateIP is
// set the created control-plane server carries a private-network IPv4 (so the join
// address resolves); otherwise it carries only its public IPv4. It returns the
// Base, the fake infra, the strategy, the injected bootstrap material, and the
// control plane's public host (the in-process SSH server's address).
func newMultiNodeBase(
	t *testing.T,
	withPrivateIP bool,
) (*hetznerbase.Base, *fakeInfra, *fakeMultiNodeStrategy, hetznerbase.BootstrapMaterial, string) {
	t.Helper()

	pair, err := sshbootstrap.GenerateKeyPair()
	require.NoError(t, err)

	host, port, hostKey := startBringUpSSHServer(
		t, pair.Signer.PublicKey(), kubeconfigHandler(0, remoteAdminKubeconfig),
	)

	server := serverWithPublicIPv4(host)
	if withPrivateIP {
		server.PrivateNet = []hcloud.ServerPrivateNet{{IP: net.ParseIP(testJoinPrivateIP)}}
	}

	infra := &fakeInfra{createdServer: server, networkID: 11}
	base := newBase(infra, v1alpha1.OptionsHetzner{})
	base.Agents = testMultiNodeAgents
	base.KubeconfigPath = filepath.Join(t.TempDir(), "kubeconfig")
	base.BringUpPort = port
	base.BringUpPollInterval = testPollInterval

	strategy := &fakeMultiNodeStrategy{
		kubeconfigPath: testKubeconfigPath,
		agents:         testMultiNodeAgents,
	}
	base.Strategy = strategy

	material := hetznerbase.BootstrapMaterial{
		Signer:          pair.Signer,
		AuthorizedKey:   pair.AuthorizedKey,
		HostKeyCallback: gossh.FixedHostKey(hostKey),
	}

	return base, infra, strategy, material, host
}

func TestRunCreateMultiNodeBringsUpControlPlaneAndCreatesAgents(t *testing.T) {
	t.Parallel()

	base, infra, strategy, material, host := newMultiNodeBase(t, true)

	ctx, cancel := context.WithTimeout(t.Context(), testBringUpBudget)
	defer cancel()

	require.NoError(t, base.RunCreateMultiNode(ctx, "", strategy, material))

	// One CreateServer for the control plane plus one per agent.
	assert.Equal(t, 1+testMultiNodeAgents, infra.createServerCalls)
	// The engine derived the join address from the control plane's private IPv4.
	require.NotNil(t, strategy.receivedJoinAddress)
	assert.Equal(t, testJoinPrivateIP, strategy.receivedJoinAddress.String())
	// A successful bring-up leaves the cluster in place.
	assert.Equal(t, 0, infra.deleteNodesCalls)

	// The control plane's kubeconfig is persisted with its endpoint rewritten to
	// the node's public IPv4.
	merged, err := os.ReadFile(base.KubeconfigPath)
	require.NoError(t, err)
	assert.Contains(t, string(merged), "https://"+host+":6443")
	assert.NotContains(t, string(merged), "10.9.9.9")
}

func TestRunCreateMultiNodeMissingPrivateIPCleansUp(t *testing.T) {
	t.Parallel()

	// The control plane comes up but has no private-network address, so the join
	// address cannot be derived and the cluster is torn down again.
	base, infra, strategy, material, _ := newMultiNodeBase(t, false)

	ctx, cancel := context.WithTimeout(t.Context(), testBringUpBudget)
	defer cancel()

	err := base.RunCreateMultiNode(ctx, "", strategy, material)
	require.ErrorIs(t, err, hetznerbase.ErrNoPrivateIPv4)
	// Only the control plane was created; no agents.
	assert.Equal(t, 1, infra.createServerCalls)
	// Cleanup-on-failure ran.
	assert.Equal(t, 1, infra.deleteNodesCalls)
}

func TestRunCreateMultiNodeComposeJoinErrorCleansUp(t *testing.T) {
	t.Parallel()

	base, infra, strategy, material, _ := newMultiNodeBase(t, true)
	strategy.composeJoinErr = errBoom

	ctx, cancel := context.WithTimeout(t.Context(), testBringUpBudget)
	defer cancel()

	err := base.RunCreateMultiNode(ctx, "", strategy, material)
	require.ErrorIs(t, err, errBoom)
	// The control plane was created before composing the joiners failed.
	assert.Equal(t, 1, infra.createServerCalls)
	assert.Equal(t, 1, infra.deleteNodesCalls)
}

func TestRunCreateMultiNodeAlreadyExists(t *testing.T) {
	t.Parallel()

	base, infra, strategy, material, _ := newMultiNodeBase(t, true)
	infra.nodesExist = true

	err := base.RunCreateMultiNode(t.Context(), "named", strategy, material)
	require.ErrorIs(t, err, hetznerbase.ErrClusterAlreadyExists)
	// No infrastructure or servers were created.
	assert.Equal(t, 0, infra.ensureNetworkCalls)
	assert.Equal(t, 0, infra.createServerCalls)
}

func TestRunCreateMultiNodeMissingKubeconfigDestinationFailsFast(t *testing.T) {
	t.Parallel()

	base, infra, strategy, material, _ := newMultiNodeBase(t, true)
	base.KubeconfigPath = ""

	err := base.RunCreateMultiNode(t.Context(), "", strategy, material)
	require.ErrorIs(t, err, hetznerbase.ErrMissingKubeconfigDestination)
	assert.Equal(t, 0, infra.ensureNetworkCalls)
	assert.Equal(t, 0, infra.createServerCalls)
}
