package hetznerbase_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sync"
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

func TestRunCreateMultiNodeComposeInitErrorFailsFast(t *testing.T) {
	t.Parallel()

	// Composing the init control plane fails before any server is created, so the
	// engine fails fast: no server is provisioned and there is nothing to tear
	// down. Cleanup-on-failure only runs once the control plane is up (as the
	// missing-private-IP and compose-join cases above assert), so a compose-init
	// failure must NOT trigger it.
	base, infra, strategy, material, _ := newMultiNodeBase(t, true)
	strategy.composeInitErr = errBoom

	err := base.RunCreateMultiNode(t.Context(), "", strategy, material)
	require.ErrorIs(t, err, errBoom)
	// No servers were created and no cleanup ran — the failure is before any
	// paid resource exists.
	assert.Equal(t, 0, infra.createServerCalls)
	assert.Equal(t, 0, infra.deleteNodesCalls)
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

// testSentinelPath is the join-complete sentinel the fake HA strategy exposes;
// testSentinelProbe is the FileExists command the engine's wait issues for it.
const (
	testSentinelPath  = "/var/lib/fake/join-complete"
	testSentinelProbe = "test -f '/var/lib/fake/join-complete'"
)

// testHAControlPlaneJoiners is the additional-control-plane count the HA
// multi-node engine tests bring up alongside the init control plane and the
// [testMultiNodeAgents] agents.
const testHAControlPlaneJoiners = 2

// fakeHAMultiNodeStrategy upgrades the multi-node fake to an
// [hetznerbase.HAControlPlaneComposer]: its joining nodes are the additional
// control planes first, then the agents — mirroring the real composers' order.
type fakeHAMultiNodeStrategy struct {
	fakeMultiNodeStrategy

	controlPlaneJoiners int
	sentinelPath        string
}

func (f *fakeHAMultiNodeStrategy) SupportsHAControlPlanes() {}

func (f *fakeHAMultiNodeStrategy) ControlPlaneJoinCompletePath() string { return f.sentinelPath }

func (f *fakeHAMultiNodeStrategy) ComposeJoiningNodes(
	_, _ string,
	joinAddress net.IP,
	_ hetznerbase.BootstrapMaterial,
) ([]hetznerbase.NodeSpec, error) {
	f.receivedJoinAddress = joinAddress

	specs := make([]hetznerbase.NodeSpec, 0, f.controlPlaneJoiners+f.agents)

	for index := 1; index <= f.controlPlaneJoiners; index++ {
		specs = append(specs, hetznerbase.NodeSpec{
			Index:    index,
			NodeType: hetzner.NodeTypeControlPlane,
			UserData: "#cloud-config\n",
		})
	}

	for index := 1; index <= f.agents; index++ {
		specs = append(specs, hetznerbase.NodeSpec{
			Index:    f.controlPlaneJoiners + index,
			NodeType: hetzner.NodeTypeWorker,
			UserData: "#cloud-config\n",
		})
	}

	return specs, nil
}

// eventLog records the interleaving of server creations (engine goroutine) and
// sentinel probes (in-process SSH server goroutines) under one mutex, so the
// serialisation assertion is race-free.
type eventLog struct {
	mu     sync.Mutex
	events []string
}

func (l *eventLog) add(event string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.events = append(l.events, event)
}

func (l *eventLog) list() []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return append([]string(nil), l.events...)
}

// newHAMultiNodeBase wires a Base whose strategy composes additional control
// planes, against an SSH server that answers the init node's kubeconfig
// protocol and reports the join-complete sentinel via sentinelHandler.
func newHAMultiNodeBase(
	t *testing.T,
	log *eventLog,
	sentinelHandler func() (string, uint32),
) (*hetznerbase.Base, *fakeInfra, *fakeHAMultiNodeStrategy, hetznerbase.BootstrapMaterial) {
	t.Helper()

	pair, err := sshbootstrap.GenerateKeyPair()
	require.NoError(t, err)

	kubeconfig := kubeconfigHandler(0, remoteAdminKubeconfig)
	host, port, hostKey := startBringUpSSHServer(
		t, pair.Signer.PublicKey(),
		func(command string) (string, uint32) {
			if command == testSentinelProbe {
				return sentinelHandler()
			}

			return kubeconfig(command)
		},
	)

	server := serverWithPublicIPv4(host)
	server.PrivateNet = []hcloud.ServerPrivateNet{{IP: net.ParseIP(testJoinPrivateIP)}}

	infra := &fakeInfra{createdServer: server, networkID: 11}
	infra.onCreateServer = func(opts hetzner.CreateServerOpts) { log.add("create:" + opts.Name) }

	base := newBase(infra, v1alpha1.OptionsHetzner{})
	base.ControlPlanes = 1 + testHAControlPlaneJoiners
	base.Agents = testMultiNodeAgents
	base.KubeconfigPath = filepath.Join(t.TempDir(), "kubeconfig")
	base.BringUpPort = port
	base.BringUpPollInterval = testPollInterval

	strategy := &fakeHAMultiNodeStrategy{
		fakeMultiNodeStrategy: fakeMultiNodeStrategy{
			kubeconfigPath: testKubeconfigPath,
			agents:         testMultiNodeAgents,
		},
		controlPlaneJoiners: testHAControlPlaneJoiners,
		sentinelPath:        testSentinelPath,
	}
	base.Strategy = strategy

	material := hetznerbase.BootstrapMaterial{
		Signer:          pair.Signer,
		AuthorizedKey:   pair.AuthorizedKey,
		HostKeyCallback: gossh.FixedHostKey(hostKey),
	}

	return base, infra, strategy, material
}

// nodeName derives the server name the engine gives clusterName's node, so the
// serialisation assertion matches on real creation events.
func nodeName(t *testing.T, clusterName, nodeType string, index int) string {
	t.Helper()

	name, err := hetzner.NodeName(clusterName, nodeType, index)
	require.NoError(t, err)

	return name
}

func TestRunCreateMultiNodeSerialisesControlPlaneJoins(t *testing.T) {
	t.Parallel()

	log := &eventLog{}
	base, infra, strategy, material := newHAMultiNodeBase(
		t, log,
		func() (string, uint32) {
			log.add("join-complete-probe")

			return "", 0
		},
	)

	ctx, cancel := context.WithTimeout(t.Context(), testBringUpBudget)
	defer cancel()

	require.NoError(t, base.RunCreateMultiNode(ctx, "", strategy, material))

	clusterName := base.ClusterName
	controlPlane := hetzner.NodeTypeControlPlane
	worker := hetzner.NodeTypeWorker

	// Each control-plane joiner's join completion is observed BEFORE the next
	// joining node is created; the agents follow without any join wait.
	assert.Equal(t, []string{
		"create:" + nodeName(t, clusterName, controlPlane, 0),
		"create:" + nodeName(t, clusterName, controlPlane, 1),
		"join-complete-probe",
		"create:" + nodeName(t, clusterName, controlPlane, 2),
		"join-complete-probe",
		"create:" + nodeName(t, clusterName, worker, 3),
		"create:" + nodeName(t, clusterName, worker, 4),
	}, log.list())

	assert.Equal(t, 0, infra.deleteNodesCalls)
}

func TestRunCreateMultiNodeControlPlaneJoinNeverCompletesCleansUp(t *testing.T) {
	t.Parallel()

	log := &eventLog{}
	base, infra, strategy, material := newHAMultiNodeBase(
		t, log,
		func() (string, uint32) { return "", errExitNotFound }, // sentinel never appears
	)

	ctx, cancel := context.WithTimeout(t.Context(), testFailFastBudget)
	defer cancel()

	err := base.RunCreateMultiNode(ctx, "", strategy, material)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, "wait for control-plane joiner")
	// Only the init control plane and the first (stuck) joiner were created —
	// the wedged join blocked every later node — and cleanup-on-failure ran.
	assert.Equal(t, 2, infra.createServerCalls)
	assert.Equal(t, 1, infra.deleteNodesCalls)
}

func TestRunCreateMultiNodeEmptyJoinSentinelCleansUp(t *testing.T) {
	t.Parallel()

	log := &eventLog{}
	base, infra, strategy, material := newHAMultiNodeBase(
		t, log,
		func() (string, uint32) { return "", 0 },
	)
	strategy.sentinelPath = ""

	ctx, cancel := context.WithTimeout(t.Context(), testBringUpBudget)
	defer cancel()

	err := base.RunCreateMultiNode(ctx, "", strategy, material)
	require.ErrorIs(t, err, hetznerbase.ErrMissingControlPlaneJoinSentinel)
	assert.Equal(t, 1, infra.deleteNodesCalls)
}
