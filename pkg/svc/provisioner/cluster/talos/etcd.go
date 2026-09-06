package talosprovisioner

import (
	"context"
	"fmt"
	"net/netip"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"google.golang.org/grpc"
)

type etcdMembershipClient interface {
	EtcdForfeitLeadership(
		ctx context.Context,
		request *machineapi.EtcdForfeitLeadershipRequest,
		options ...grpc.CallOption,
	) (*machineapi.EtcdForfeitLeadershipResponse, error)
	EtcdLeaveCluster(
		ctx context.Context,
		request *machineapi.EtcdLeaveClusterRequest,
		options ...grpc.CallOption,
	) error
	Close() error
}

func (p *Provisioner) openEtcdMembershipClient(
	ctx context.Context,
	nodeIP string,
) (etcdMembershipClient, error) {
	if p.etcdClientFactory != nil {
		return p.etcdClientFactory(ctx, nodeIP)
	}

	return p.dialTalosClientWithRetry(ctx, nodeIP, "etcd cleanup connect")
}

// etcdCleanupBeforeRemoval requires successful etcd membership cleanup before
// removing a control-plane node. It connects to the node, forfeits leadership
// if the node is the leader, then tells the node to leave the etcd cluster.
//
// Connection and membership-removal errors retain the infrastructure. A failed
// membership RPC may have taken effect, so callers must not blindly retry it or
// assume that deleting the node is safe.
func (p *Provisioner) etcdCleanupBeforeRemoval(
	ctx context.Context,
	nodeIP string,
) error {
	_, addressErr := netip.ParseAddr(nodeIP)
	if addressErr != nil {
		return fmt.Errorf("invalid control-plane address for etcd cleanup: %w", addressErr)
	}

	_, _ = fmt.Fprintf(p.logWriter,
		"  Cleaning up etcd membership for %s...\n", nodeIP)

	// EtcdForfeitLeadership/EtcdLeaveCluster are not idempotent, so the transient
	// apid handshake race is absorbed by the Version probe inside
	// dialTalosClientWithRetry and each membership RPC is issued exactly once.
	client, err := p.openEtcdMembershipClient(ctx, nodeIP)
	if err != nil {
		return fmt.Errorf("connect to %s for etcd cleanup: %w", nodeIP, err)
	}

	defer client.Close() //nolint:errcheck

	// Step 1: Attempt leadership transfer. Successful leave below is the required
	// membership boundary even when this preliminary transfer fails.
	_, err = client.EtcdForfeitLeadership(
		ctx,
		&machineapi.EtcdForfeitLeadershipRequest{},
	)
	if err != nil {
		_, _ = fmt.Fprintf(p.logWriter,
			"  ⚠ Forfeit leadership failed on %s (best-effort): %v\n",
			nodeIP, err)
	}

	// Step 2: Tell the node to leave the etcd cluster.
	err = client.EtcdLeaveCluster(
		ctx,
		&machineapi.EtcdLeaveClusterRequest{},
	)
	if err != nil {
		return fmt.Errorf("remove etcd membership on %s: %w", nodeIP, err)
	}

	_, _ = fmt.Fprintf(p.logWriter,
		"  ✓ Etcd member removed from cluster (%s)\n", nodeIP)

	return nil
}
