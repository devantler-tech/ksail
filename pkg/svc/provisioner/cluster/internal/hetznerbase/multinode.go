package hetznerbase

import (
	"context"
	"errors"
	"fmt"
	"net"

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

// MultiNodeComposer is the optional capability a distribution's [CreateStrategy]
// implements once it can bring up joining nodes. The two-phase multi-node create
// flow ([Base.RunCreateMultiNode]) needs the init control plane composed on its
// own (before any address is known) and the joining nodes composed afterwards
// against the init control plane's resolved private address — a distribution
// whose bootstrap cannot yet thread that join endpoint (kubeadm) simply does not
// implement this, and a topology with agents is rejected before any resource is
// created. See devantler-tech/ksail#5755.
type MultiNodeComposer interface {
	// ComposeInitNode composes the single cluster-initialising control-plane node
	// (bootstrap index 0). It carries no join endpoint — it initialises the
	// cluster the joining nodes later register against.
	ComposeInitNode(clusterName, token string, material BootstrapMaterial) (NodeSpec, error)
	// ComposeJoiningNodes composes the joining nodes (agents; additional control
	// planes are a later increment) that register against the control plane
	// reachable at joinAddress — the init node's private-network IPv4. The
	// distribution forms its own registration URL from joinAddress (both k3s and
	// kubeadm serve the API on the standard secure port). The returned specs carry
	// their global bootstrap indices (>= 1) so their server names stay distinct
	// from the init node's.
	ComposeJoiningNodes(
		clusterName, token string,
		joinAddress net.IP,
		material BootstrapMaterial,
	) ([]NodeSpec, error)
}

// RunCreateMultiNode runs the two-phase Hetzner create flow for a single
// control-plane cluster with joining nodes (agents): guard against an existing
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
// Joining nodes are created and left to register asynchronously — the cluster is
// usable once the control plane serves the API and its kubeconfig is retrieved;
// end-to-end join success is validated by the live smoke tests
// (devantler-tech/ksail#5515) once the Hetzner CI lane (#4972) is restored.
func (b *Base) RunCreateMultiNode(
	ctx context.Context,
	name string,
	composer MultiNodeComposer,
	material BootstrapMaterial,
) error {
	clusterName := b.ResolveName(name)

	exists, err := b.Infra.NodesExist(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("check existing nodes: %w", err)
	}

	if exists {
		return fmt.Errorf("%w: %s", ErrClusterAlreadyExists, clusterName)
	}

	// Fail before any paid resource exists when the retrieved kubeconfig would
	// have nowhere to go.
	if b.KubeconfigPath == "" {
		return ErrMissingKubeconfigDestination
	}

	infra, err := b.EnsureInfrastructure(ctx, clusterName)
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

	return b.persistInitKubeconfig(ctx, clusterName, initResult)
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
// private-network address and creates them. They bootstrap and register
// asynchronously, so it does not wait for a kubeconfig from them; a creation
// failure tears the whole cluster down.
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

	for _, spec := range joinSpecs {
		_, createErr := b.Servers.CreateServer(ctx, spec)
		if createErr != nil {
			return b.cleanUpFailedBringUp(
				ctx,
				clusterName,
				fmt.Errorf("create joining node %q: %w", spec.Name, createErr),
			)
		}
	}

	return nil
}

// persistInitKubeconfig rewrites the init control plane's kubeconfig endpoint to
// its public IPv4 and merges it into the Base's kubeconfig destination, sharing
// the single-node path's cleanup-on-failure semantics.
func (b *Base) persistInitKubeconfig(
	ctx context.Context,
	clusterName string,
	initResult BringUpResult,
) error {
	endpoint, err := apiServerEndpoint(initResult.Server)
	if err != nil {
		return b.cleanUpFailedBringUp(ctx, clusterName, err)
	}

	kubeconfig, err := rewriteKubeconfigEndpoint(initResult.Kubeconfig, endpoint)
	if err != nil {
		return b.cleanUpFailedBringUp(ctx, clusterName, err)
	}

	persistedPath, err := b.persistKubeconfig(kubeconfig)
	if err != nil {
		return b.cleanUpFailedBringUp(ctx, clusterName, err)
	}

	_, _ = fmt.Fprintf(
		b.LogWriter,
		"Cluster %q control plane is up at %s; joining nodes are registering; "+
			"kubeconfig merged into %q\n",
		clusterName, endpoint, persistedPath,
	)

	return nil
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
