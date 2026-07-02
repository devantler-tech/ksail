package hetznerbase

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	sshbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/ssh"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	gossh "golang.org/x/crypto/ssh"
)

// DefaultKubeconfigPollInterval is the default delay between probes for the
// remote kubeconfig while the node's first-boot bootstrap (cloud-init running
// the k3s or kubeadm install) is still writing it.
const DefaultKubeconfigPollInterval = 3 * time.Second

// sshPort is the port the bootstrap SSH dial connects to on a freshly created
// server (stock OS images run sshd on the standard port).
const sshPort = "22"

// bootstrapUser is the user the bootstrap SSH dial authenticates as. Hetzner
// stock OS images deliver cloud-init ssh_authorized_keys to root.
const bootstrapUser = "root"

var (
	// ErrNoPublicIPv4 is returned by [Base.BringUpNode] when the created server
	// carries no public IPv4 address to dial the bootstrap SSH connection to.
	// IPv4-less topologies need a private-network dial path, which the bring-up
	// engine does not implement yet.
	ErrNoPublicIPv4 = errors.New(
		"hetzner: created server has no public IPv4 address for the SSH bootstrap",
	)

	// ErrMissingKubeconfigPath is returned by [Base.BringUpNode] when the spec
	// names no remote kubeconfig path to wait for.
	ErrMissingKubeconfigPath = errors.New("hetzner: bring-up spec is missing the kubeconfig path")
)

// BringUpPlan is what a provisioner's composePlan callback returns to
// [Base.RunCreate]: the ordered composed server specs plus the bootstrap
// material the live bring-up authenticates with and the remote location the
// node's first boot writes the admin kubeconfig to. The current increment
// carries exactly one spec (multi-node topologies are rejected before
// composition); the ordered slice is the multi-node join sequencing's seam.
type BringUpPlan struct {
	// Specs is the ordered composed server specs (index 0 is the
	// cluster-initialising control plane). Each spec's UserData must deliver
	// the public half of the bootstrap keypair (see [BringUpSpec.Server]).
	Specs []hetzner.CreateServerOpts
	// Signer is the private half of the bootstrap keypair (see
	// [BringUpSpec.Signer]).
	Signer gossh.Signer
	// HostKeyCallback is the host-key trust policy (see
	// [BringUpSpec.HostKeyCallback]).
	HostKeyCallback gossh.HostKeyCallback
	// RemoteKubeconfigPath is the remote path of the admin kubeconfig the
	// node's bootstrap writes (see [BringUpSpec.KubeconfigPath]).
	RemoteKubeconfigPath string
	// PollInterval is the delay between kubeconfig probes; zero means
	// [DefaultKubeconfigPollInterval].
	PollInterval time.Duration
	// Port is the SSH port dialed on the node; empty means the standard port.
	Port string
}

// BringUpSpec carries everything [Base.BringUpNode] needs to take one composed
// server spec to a running node and its admin kubeconfig.
type BringUpSpec struct {
	// Server is the composed server spec. Its UserData must deliver the public
	// half of the bootstrap keypair into the node's authorized_keys (the
	// cloud-init ssh_authorized_keys module) — otherwise the SSH dial below can
	// never authenticate.
	Server hetzner.CreateServerOpts
	// Signer is the private half of the bootstrap keypair, authenticating the
	// SSH dial.
	Signer gossh.Signer
	// HostKeyCallback verifies the server's SSH host key. The trust policy is
	// the caller's decision (mirroring [sshbootstrap.Options]): a caller that
	// delivers pre-generated host keys via user_data can pin them with
	// [gossh.FixedHostKey]; a caller accepting first-boot trust supplies its own
	// accept policy.
	HostKeyCallback gossh.HostKeyCallback
	// KubeconfigPath is the remote path of the admin kubeconfig the node's
	// bootstrap writes (k3s: /etc/rancher/k3s/k3s.yaml, kubeadm:
	// /etc/kubernetes/admin.conf).
	KubeconfigPath string
	// PollInterval is the delay between probes for KubeconfigPath; zero means
	// [DefaultKubeconfigPollInterval].
	PollInterval time.Duration
	// Port is the SSH port dialed on the node; empty means the standard port 22
	// (stock OS images run sshd there).
	Port string
}

// BringUpResult is the outcome of a successful [Base.BringUpNode]: the created
// server and the admin kubeconfig read off it.
type BringUpResult struct {
	// Server is the created Hetzner server.
	Server *hcloud.Server
	// Kubeconfig is the raw admin kubeconfig read from the node. Its API-server
	// endpoint is whatever the distribution wrote (typically a loopback or
	// private address) — rewriting it for external access is the caller's
	// concern.
	Kubeconfig []byte
}

// BringUpNode is the single-node live bring-up engine: it creates the server
// from the composed spec, dials SSH on the server's public IPv4 with the
// bootstrap keypair (retrying until the first boot brings sshd up), waits for
// the distribution's kubeconfig to exist, and reads it back. When any step
// after server creation fails, the cluster's servers are deleted again
// (cleanup-on-failure) so a failed bring-up does not leak paid resources, and
// both the cause and any cleanup failure are surfaced.
//
// The engine is deliberately policy-free: the spec's UserData, host-key trust,
// and the kubeconfig's onward handling (endpoint rewrite, persistence) belong
// to the caller. Wiring it into [Base.RunCreate] lands with the API-endpoint
// design increment (devantler-tech/ksail#5720 scopes this boundary).
func (b *Base) BringUpNode(
	ctx context.Context,
	clusterName string,
	spec BringUpSpec,
) (BringUpResult, error) {
	if spec.KubeconfigPath == "" {
		return BringUpResult{}, ErrMissingKubeconfigPath
	}

	server, err := b.Servers.CreateServer(ctx, spec.Server)
	if err != nil {
		return BringUpResult{}, fmt.Errorf("create server: %w", err)
	}

	kubeconfig, err := b.bootstrapAndReadKubeconfig(ctx, server, spec)
	if err != nil {
		return BringUpResult{}, b.cleanUpFailedBringUp(ctx, clusterName, err)
	}

	return BringUpResult{Server: server, Kubeconfig: kubeconfig}, nil
}

// bootstrapAndReadKubeconfig runs the post-create half of the bring-up: dial
// SSH on the server's public IPv4, wait for the kubeconfig, read it.
func (b *Base) bootstrapAndReadKubeconfig(
	ctx context.Context,
	server *hcloud.Server,
	spec BringUpSpec,
) ([]byte, error) {
	addr, err := publicSSHAddr(server, spec.Port)
	if err != nil {
		return nil, err
	}

	client, err := sshbootstrap.DialWithRetry(ctx, sshbootstrap.Options{
		Addr:            addr,
		User:            bootstrapUser,
		Signer:          spec.Signer,
		HostKeyCallback: spec.HostKeyCallback,
	}, 0)
	if err != nil {
		return nil, fmt.Errorf("dial bootstrap SSH at %s: %w", addr, err)
	}

	defer func() { _ = client.Close() }()

	err = waitForRemoteFile(ctx, client, spec.KubeconfigPath, spec.PollInterval)
	if err != nil {
		return nil, err
	}

	kubeconfig, err := client.ReadFile(ctx, spec.KubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("read kubeconfig %q: %w", spec.KubeconfigPath, err)
	}

	return kubeconfig, nil
}

// cleanUpFailedBringUp deletes the cluster's servers after a failed bring-up so
// a half-provisioned node does not keep accruing cost, and joins any cleanup
// failure onto the original cause. It deletes with a cancel-free context: the
// bring-up often fails *because* ctx was cancelled, and the cleanup must still
// run then.
func (b *Base) cleanUpFailedBringUp(ctx context.Context, clusterName string, cause error) error {
	deleteErr := b.Infra.DeleteNodes(context.WithoutCancel(ctx), clusterName)
	if deleteErr != nil {
		return errors.Join(
			cause,
			fmt.Errorf("clean up servers after failed bring-up: %w", deleteErr),
		)
	}

	return cause
}

// publicSSHAddr derives the "host:port" the bootstrap SSH dial connects to from
// the created server's public IPv4, or [ErrNoPublicIPv4] when the server has
// none (IPv4 disabled or not yet assigned). An empty port means the standard
// SSH port.
func publicSSHAddr(server *hcloud.Server, port string) (string, error) {
	address, err := publicIPv4(server)
	if err != nil {
		return "", err
	}

	if port == "" {
		port = sshPort
	}

	return net.JoinHostPort(address.String(), port), nil
}

// publicIPv4 returns the created server's public IPv4, or [ErrNoPublicIPv4]
// when the server has none (IPv4 disabled or not yet assigned).
func publicIPv4(server *hcloud.Server) (net.IP, error) {
	if server == nil || server.PublicNet.IPv4.IP == nil ||
		server.PublicNet.IPv4.IP.IsUnspecified() {
		return nil, ErrNoPublicIPv4
	}

	return server.PublicNet.IPv4.IP, nil
}

// waitForRemoteFile polls the remote node until path exists
// ([sshbootstrap.Client.FileExists]), sleeping interval between probes and
// giving up when ctx ends. "Does not exist yet" retries; a probe error is
// surfaced immediately — the connection is already established, so transport
// errors are real failures, not boot-time races.
func waitForRemoteFile(
	ctx context.Context,
	client *sshbootstrap.Client,
	path string,
	interval time.Duration,
) error {
	if interval <= 0 {
		interval = DefaultKubeconfigPollInterval
	}

	for {
		exists, err := client.FileExists(ctx, path)
		if err != nil {
			return fmt.Errorf("wait for remote file: %w", err)
		}

		if exists {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for remote file %q: %w", path, ctx.Err())
		case <-time.After(interval):
		}
	}
}
