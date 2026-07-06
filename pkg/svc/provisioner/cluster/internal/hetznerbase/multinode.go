package hetznerbase

import (
	"context"
	"errors"
	"fmt"
	"net"

	sshbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/ssh"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// ErrNoPrivateIPv4 is returned by [Base.RunCreateMultiNode] when the created
// cluster-initialising control plane has no private-network address for the
// joining nodes to register against. Joining nodes reach the control plane over
// the cluster's private network (unfiltered by the cloud firewall), so a missing
// private IP means the network placement did not take and the join would fail.
var ErrNoPrivateIPv4 = errors.New(
	"hetzner: cluster-initialising control plane has no private-network IPv4 for joining nodes",
)

// ErrMissingControlPlaneJoinSentinel is returned by [Base.RunCreateMultiNode]
// when an [HAControlPlaneComposer] supplies no join-complete sentinel path for
// its control-plane joiners — without one their joins cannot be serialised.
var ErrMissingControlPlaneJoinSentinel = errors.New(
	"hetzner: control-plane join-complete sentinel path is empty",
)

// MultiNodeComposer is the optional capability a distribution's [CreateStrategy]
// implements once it can bring up joining nodes. The two-phase multi-node create
// flow ([Base.RunCreateMultiNode]) needs the init control plane composed on its
// own (before any address is known) and the joining nodes composed afterwards
// against the init control plane's resolved private address — a distribution
// whose bootstrap cannot yet thread that join endpoint simply does not
// implement this, and a topology with agents is rejected before any resource is
// created. See devantler-tech/ksail#5755.
type MultiNodeComposer interface {
	// ComposeInitNode composes the single cluster-initialising control-plane node
	// (bootstrap index 0). It carries no join endpoint — it initialises the
	// cluster the joining nodes later register against.
	ComposeInitNode(clusterName, token string, material BootstrapMaterial) (NodeSpec, error)
	// ComposeJoiningNodes composes the joining nodes that register against the
	// control plane reachable at joinAddress — the init node's private-network
	// IPv4. For every distribution that means the agents; a distribution that
	// also implements [HAControlPlaneComposer] composes its additional control
	// planes here too. The distribution forms its own registration URL from joinAddress
	// (both k3s and kubeadm serve the API on the standard secure port). The
	// returned specs carry their global bootstrap indices (>= 1) so their server
	// names stay distinct from the init node's.
	ComposeJoiningNodes(
		clusterName, token string,
		joinAddress net.IP,
		material BootstrapMaterial,
	) ([]NodeSpec, error)
}

// HAControlPlaneComposer is the optional capability a distribution's
// [MultiNodeComposer] additionally implements once its compose halves handle a
// multi-control-plane (high-availability) topology: the init node advertises a
// stable control-plane endpoint and [MultiNodeComposer.ComposeJoiningNodes]
// composes the additional control planes as control-plane joiners alongside the
// agents. [Base.Create] rejects controlPlanes > 1 with
// [ErrHAControlPlaneNotImplemented] for any strategy that does not implement
// this — the guard is per-distribution because the join mechanics differ:
// kubeadm's manual certificate distribution (devantler-tech/ksail#5796) and
// k3s's embedded etcd (devantler-tech/ksail#5854) each implement it their own
// way.
type HAControlPlaneComposer interface {
	MultiNodeComposer

	// SupportsHAControlPlanes marks the capability; it performs no work. A
	// distribution declares — by implementing this — that its ComposeJoiningNodes
	// output is correct for additional control planes, not only agents.
	SupportsHAControlPlanes()

	// ControlPlaneJoinCompletePath returns the remote file path whose existence
	// marks a joining control plane's bootstrap complete — a file the
	// distribution's first boot writes only after its join command succeeded
	// (never a file the join writes part-way, which would report completion
	// while etcd membership is still changing). [Base.RunCreateMultiNode] polls
	// it over SSH to serialise control-plane joins.
	ControlPlaneJoinCompletePath() string
}

// RunCreateMultiNode runs the two-phase Hetzner create flow for a cluster with
// joining nodes (agents and — for an [HAControlPlaneComposer] strategy —
// additional control planes): guard against an existing
// cluster, ensure the shared infrastructure, then (1) bring up the
// cluster-initialising control plane and read its admin kubeconfig, and (2)
// compose the joining nodes against the control plane's private-network address
// and create them. The control plane's public IPv4 is the kubeconfig endpoint,
// as in the single-node path; the joining nodes register over the private
// network. A failure after the first server is created tears the whole cluster
// down (cleanup-on-failure) so a partial bring-up does not leak paid resources.
//
// material is the per-cluster bootstrap material the live bring-up authenticates
// with (minted by [Base.Create] and injected here, mirroring how [Base.RunCreate]
// takes an already-composed plan) so the SSH bring-up is testable with an
// in-process server.
//
// Agents are created and left to register asynchronously — the cluster is
// usable once the control plane serves the API and its kubeconfig is retrieved;
// end-to-end join success is validated by the live smoke tests
// (devantler-tech/ksail#5515) once the Hetzner CI lane (#4972) is restored.
// Additional control planes are serialised instead: each control-plane joiner's
// join-complete sentinel ([HAControlPlaneComposer.ControlPlaneJoinCompletePath])
// is awaited over SSH before the next joining node is created, because
// concurrent control-plane joins race etcd member addition
// (devantler-tech/ksail#5818).
func (b *Base) RunCreateMultiNode(
	ctx context.Context,
	name string,
	composer MultiNodeComposer,
	material BootstrapMaterial,
) error {
	clusterName := b.ResolveName(name)

	infra, err := b.guardCreate(ctx, clusterName)
	if err != nil {
		return err
	}

	token, err := b.Strategy.GenerateToken()
	if err != nil {
		return fmt.Errorf("generate node token: %w", err)
	}

	initResult, err := b.bringUpInitControlPlane(ctx, clusterName, infra, token, material, composer)
	if err != nil {
		return err
	}

	err = b.createJoiningNodes(
		ctx,
		clusterName,
		infra,
		token,
		material,
		composer,
		initResult.Server,
	)
	if err != nil {
		return err
	}

	endpoint, persistedPath, err := b.rewriteAndPersistKubeconfig(ctx, clusterName, initResult)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(
		b.LogWriter,
		"Cluster %q control plane is up at %s; joining nodes are registering; "+
			"kubeconfig merged into %q\n",
		clusterName, endpoint, persistedPath,
	)

	return nil
}

// bringUpInitControlPlane composes and brings up the cluster-initialising
// control plane, returning its bring-up result (created server + admin
// kubeconfig).
func (b *Base) bringUpInitControlPlane(
	ctx context.Context,
	clusterName string,
	infra ResolvedInfra,
	token string,
	material BootstrapMaterial,
	composer MultiNodeComposer,
) (BringUpResult, error) {
	initNode, err := composer.ComposeInitNode(clusterName, token, material)
	if err != nil {
		return BringUpResult{}, fmt.Errorf("compose init control-plane node: %w", err)
	}

	initSpecs, err := DeriveServerSpecs(clusterName, []NodeSpec{initNode}, b.Opts, infra)
	if err != nil {
		return BringUpResult{}, err
	}

	return b.BringUpNode(ctx, clusterName, BringUpSpec{
		Server:          initSpecs[0],
		Signer:          material.Signer,
		HostKeyCallback: material.HostKeyCallback,
		KubeconfigPath:  b.Strategy.RemoteKubeconfigPath(),
		PollInterval:    b.BringUpPollInterval,
		Port:            b.BringUpPort,
	})
}

// createJoiningNodes composes the joining nodes against the init control plane's
// private-network address and creates them. Agents bootstrap and register
// asynchronously, so it does not wait for a kubeconfig from them; a
// control-plane joiner's join-complete sentinel is awaited before the next
// joining node is created (etcd member addition must not be raced). A creation
// or join failure tears the whole cluster down.
func (b *Base) createJoiningNodes(
	ctx context.Context,
	clusterName string,
	infra ResolvedInfra,
	token string,
	material BootstrapMaterial,
	composer MultiNodeComposer,
	initServer *hcloud.Server,
) error {
	joinAddress, err := privateIPv4(initServer)
	if err != nil {
		return b.cleanUpFailedBringUp(ctx, clusterName, err)
	}

	joinNodes, err := composer.ComposeJoiningNodes(clusterName, token, joinAddress, material)
	if err != nil {
		return b.cleanUpFailedBringUp(ctx, clusterName, err)
	}

	joinSpecs, err := DeriveServerSpecs(clusterName, joinNodes, b.Opts, infra)
	if err != nil {
		return b.cleanUpFailedBringUp(ctx, clusterName, err)
	}

	haComposer, _ := composer.(HAControlPlaneComposer)

	for index, spec := range joinSpecs {
		server, createErr := b.Servers.CreateServer(ctx, spec)
		if createErr != nil {
			return b.cleanUpFailedBringUp(
				ctx,
				clusterName,
				fmt.Errorf("create joining node %q: %w", spec.Name, createErr),
			)
		}

		// joinSpecs is derived 1:1 in order from joinNodes, so the node's role
		// travels by index.
		if haComposer == nil || joinNodes[index].NodeType != hetzner.NodeTypeControlPlane {
			continue
		}

		waitErr := b.waitForControlPlaneJoin(ctx, server, material, haComposer)
		if waitErr != nil {
			return b.cleanUpFailedBringUp(
				ctx,
				clusterName,
				fmt.Errorf("wait for control-plane joiner %q: %w", spec.Name, waitErr),
			)
		}
	}

	return nil
}

// waitForControlPlaneJoin dials the created control-plane joiner over SSH with
// the cluster's bootstrap material and waits for its join-complete sentinel, so
// the next joining node is not created while this one's etcd member addition is
// still in flight.
func (b *Base) waitForControlPlaneJoin(
	ctx context.Context,
	server *hcloud.Server,
	material BootstrapMaterial,
	haComposer HAControlPlaneComposer,
) error {
	sentinelPath := haComposer.ControlPlaneJoinCompletePath()
	if sentinelPath == "" {
		return ErrMissingControlPlaneJoinSentinel
	}

	addr, err := publicSSHAddr(server, b.BringUpPort)
	if err != nil {
		return err
	}

	client, err := sshbootstrap.DialWithRetry(ctx, sshbootstrap.Options{
		Addr:            addr,
		User:            bootstrapUser,
		Signer:          material.Signer,
		HostKeyCallback: material.HostKeyCallback,
	}, 0)
	if err != nil {
		return fmt.Errorf("dial bootstrap SSH at %s: %w", addr, err)
	}

	defer func() { _ = client.Close() }()

	return waitForRemoteFile(ctx, client, sentinelPath, b.BringUpPollInterval)
}

// privateIPv4 returns the created server's first private-network IPv4, or
// [ErrNoPrivateIPv4] when it has none (the network placement did not take).
func privateIPv4(server *hcloud.Server) (net.IP, error) {
	if server == nil {
		return nil, ErrNoPrivateIPv4
	}

	for _, private := range server.PrivateNet {
		if private.IP != nil && !private.IP.IsUnspecified() {
			return private.IP, nil
		}
	}

	return nil, ErrNoPrivateIPv4
}
