package hetznerbase_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	sshbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/ssh"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"
)

const (
	testKubeconfigPath  = "/etc/rancher/k3s/k3s.yaml"
	testKubeconfig      = "apiVersion: v1\nkind: Config\n"
	testProbeCommand    = "test -f '/etc/rancher/k3s/k3s.yaml'"
	testReadCommand     = "cat -- '/etc/rancher/k3s/k3s.yaml'"
	testBringUpBudget   = 10 * time.Second
	testFailFastBudget  = 400 * time.Millisecond
	testPollInterval    = 5 * time.Millisecond
	errExitNotFound     = 1
	errExitUnknownProbe = 2
)

var errUnknownTestClientKey = errors.New("unknown client key")

// bringUpExecHandler scripts the in-process SSH server's response to one exec
// request during a bring-up test.
type bringUpExecHandler func(command string) (stdout string, exitCode uint32)

// sshExitStatusPayload is the SSH exit-status request payload (RFC 4254 §6.10).
type sshExitStatusPayload struct {
	Status uint32
}

// sshExecRequestPayload is the SSH exec request payload (RFC 4254 §6.5).
type sshExecRequestPayload struct {
	Command string
}

// startBringUpSSHServer runs a minimal in-process SSH server that authenticates
// clientKey and answers exec requests via handler, returning the listener's
// host and port and the server's host key.
func startBringUpSSHServer(
	t *testing.T,
	clientKey gossh.PublicKey,
	handler bringUpExecHandler,
) (string, string, gossh.PublicKey) {
	t.Helper()

	hostPair, err := sshbootstrap.GenerateKeyPair()
	require.NoError(t, err)

	config := &gossh.ServerConfig{
		PublicKeyCallback: func(
			_ gossh.ConnMetadata,
			key gossh.PublicKey,
		) (*gossh.Permissions, error) {
			if !bytes.Equal(key.Marshal(), clientKey.Marshal()) {
				return nil, errUnknownTestClientKey
			}

			return &gossh.Permissions{}, nil
		},
	}
	config.AddHostKey(hostPair.Signer)

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		for {
			netConn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}

			go serveBringUpConn(netConn, config, handler)
		}
	}()

	host, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)

	return host, port, hostPair.Signer.PublicKey()
}

// serveBringUpConn handles one SSH connection: session channels carrying exec
// requests.
func serveBringUpConn(
	netConn net.Conn,
	config *gossh.ServerConfig,
	handler bringUpExecHandler,
) {
	_, channels, requests, err := gossh.NewServerConn(netConn, config)
	if err != nil {
		return
	}

	go gossh.DiscardRequests(requests)

	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(gossh.UnknownChannelType, "only sessions are supported")

			continue
		}

		channel, channelRequests, acceptErr := newChannel.Accept()
		if acceptErr != nil {
			continue
		}

		go serveBringUpSession(channel, channelRequests, handler)
	}
}

// serveBringUpSession answers the first exec request on one session channel.
func serveBringUpSession(
	channel gossh.Channel,
	requests <-chan *gossh.Request,
	handler bringUpExecHandler,
) {
	defer func() { _ = channel.Close() }()

	for request := range requests {
		if request.Type != "exec" {
			_ = request.Reply(false, nil)

			continue
		}

		var payload sshExecRequestPayload

		err := gossh.Unmarshal(request.Payload, &payload)
		if err != nil {
			_ = request.Reply(false, nil)

			continue
		}

		_ = request.Reply(true, nil)

		stdout, exitCode := handler(payload.Command)

		_, _ = channel.Write([]byte(stdout))
		_, _ = channel.SendRequest(
			"exit-status", false, gossh.Marshal(sshExitStatusPayload{Status: exitCode}),
		)

		return
	}
}

// serverWithPublicIPv4 builds the hcloud server the fake Infra hands back: one
// with the test SSH server's loopback address as its public IPv4.
func serverWithPublicIPv4(host string) *hcloud.Server {
	server := &hcloud.Server{}
	server.PublicNet.IPv4.IP = net.ParseIP(host)

	return server
}

// kubeconfigHandler scripts a node whose kubeconfig appears after notReadyProbes
// probes and then serves its content.
func kubeconfigHandler(notReadyProbes int32) bringUpExecHandler {
	var probes atomic.Int32

	return func(command string) (string, uint32) {
		switch command {
		case testProbeCommand:
			if probes.Add(1) <= notReadyProbes {
				return "", errExitNotFound
			}

			return "", 0
		case testReadCommand:
			return testKubeconfig, 0
		default:
			return "", errExitUnknownProbe
		}
	}
}

// bringUpSpec composes a valid BringUpSpec against the in-process SSH server.
func bringUpSpec(
	pair sshbootstrap.KeyPair,
	hostKey gossh.PublicKey,
	port string,
) hetznerbase.BringUpSpec {
	return hetznerbase.BringUpSpec{
		Signer:          pair.Signer,
		HostKeyCallback: gossh.FixedHostKey(hostKey),
		KubeconfigPath:  testKubeconfigPath,
		PollInterval:    testPollInterval,
		Port:            port,
	}
}

func TestBringUpNodeRetrievesKubeconfig(t *testing.T) {
	t.Parallel()

	pair, err := sshbootstrap.GenerateKeyPair()
	require.NoError(t, err)

	host, port, hostKey := startBringUpSSHServer(
		t, pair.Signer.PublicKey(), kubeconfigHandler(2),
	)

	infra := &fakeInfra{createdServer: serverWithPublicIPv4(host)}
	base := newBase(infra, v1alpha1.OptionsHetzner{})

	ctx, cancel := context.WithTimeout(t.Context(), testBringUpBudget)
	defer cancel()

	result, err := base.BringUpNode(ctx, testClusterName, bringUpSpec(pair, hostKey, port))
	require.NoError(t, err)
	assert.Equal(t, []byte(testKubeconfig), result.Kubeconfig)
	assert.Same(t, infra.createdServer, result.Server)
	assert.Equal(t, 1, infra.createServerCalls)
	assert.Equal(t, 0, infra.deleteNodesCalls)
}

func TestBringUpNodeMissingKubeconfigPath(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{}
	base := newBase(infra, v1alpha1.OptionsHetzner{})

	spec := hetznerbase.BringUpSpec{}

	_, err := base.BringUpNode(t.Context(), testClusterName, spec)
	require.ErrorIs(t, err, hetznerbase.ErrMissingKubeconfigPath)
	assert.Equal(t, 0, infra.createServerCalls)
}

func TestBringUpNodeCreateServerErrorSkipsCleanup(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{createServerErr: errBoom}
	base := newBase(infra, v1alpha1.OptionsHetzner{})

	spec := hetznerbase.BringUpSpec{KubeconfigPath: testKubeconfigPath}

	_, err := base.BringUpNode(t.Context(), testClusterName, spec)
	require.ErrorIs(t, err, errBoom)
	assert.Equal(t, 0, infra.deleteNodesCalls)
}

func TestBringUpNodeWithoutPublicIPv4CleansUp(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{createdServer: &hcloud.Server{}}
	base := newBase(infra, v1alpha1.OptionsHetzner{})

	spec := hetznerbase.BringUpSpec{KubeconfigPath: testKubeconfigPath}

	_, err := base.BringUpNode(t.Context(), testClusterName, spec)
	require.ErrorIs(t, err, hetznerbase.ErrNoPublicIPv4)
	assert.Equal(t, 1, infra.deleteNodesCalls)
}

func TestBringUpNodeCleanupFailureJoinsBothErrors(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{createdServer: &hcloud.Server{}, deleteNodesErr: errBoom}
	base := newBase(infra, v1alpha1.OptionsHetzner{})

	spec := hetznerbase.BringUpSpec{KubeconfigPath: testKubeconfigPath}

	_, err := base.BringUpNode(t.Context(), testClusterName, spec)
	require.ErrorIs(t, err, hetznerbase.ErrNoPublicIPv4)
	require.ErrorIs(t, err, errBoom)
	assert.Equal(t, 1, infra.deleteNodesCalls)
}

func TestBringUpNodeDialFailureCleansUp(t *testing.T) {
	t.Parallel()

	pair, err := sshbootstrap.GenerateKeyPair()
	require.NoError(t, err)

	// A listener that is closed immediately: the dial gets connection-refused
	// until the short ctx budget ends.
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	host, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	require.NoError(t, listener.Close())

	infra := &fakeInfra{createdServer: serverWithPublicIPv4(host)}
	base := newBase(infra, v1alpha1.OptionsHetzner{})

	// The dial never completes, so host-key verification is never reached; any
	// pinned key satisfies the required callback.
	spec := hetznerbase.BringUpSpec{
		Signer:          pair.Signer,
		HostKeyCallback: gossh.FixedHostKey(pair.Signer.PublicKey()),
		KubeconfigPath:  testKubeconfigPath,
		Port:            port,
	}

	ctx, cancel := context.WithTimeout(t.Context(), testFailFastBudget)
	defer cancel()

	_, err = base.BringUpNode(ctx, testClusterName, spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dial bootstrap SSH")
	assert.Equal(t, 1, infra.deleteNodesCalls)
}

func TestBringUpNodeKubeconfigNeverAppearsCleansUp(t *testing.T) {
	t.Parallel()

	pair, err := sshbootstrap.GenerateKeyPair()
	require.NoError(t, err)

	neverReady := func(command string) (string, uint32) {
		if command == testProbeCommand {
			return "", errExitNotFound
		}

		return "", errExitUnknownProbe
	}

	host, port, hostKey := startBringUpSSHServer(t, pair.Signer.PublicKey(), neverReady)

	infra := &fakeInfra{createdServer: serverWithPublicIPv4(host)}
	base := newBase(infra, v1alpha1.OptionsHetzner{})

	ctx, cancel := context.WithTimeout(t.Context(), testFailFastBudget)
	defer cancel()

	_, err = base.BringUpNode(ctx, testClusterName, bringUpSpec(pair, hostKey, port))
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, 1, infra.deleteNodesCalls)
}
