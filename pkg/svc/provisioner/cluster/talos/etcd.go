package talosprovisioner

import (
	"context"
	"fmt"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
)

// etcdCleanupBeforeRemoval performs best-effort etcd membership cleanup before
// removing a control-plane node. It connects to the node, forfeits leadership
// if the node is the leader, then tells the node to leave the etcd cluster.
//
// All errors are logged but not returned — the node removal should proceed
// regardless of whether etcd cleanup succeeded.
func (p *Provisioner) etcdCleanupBeforeRemoval(
	ctx context.Context,
	nodeIP string,
) {
	_, _ = fmt.Fprintf(p.logWriter,
		"  Cleaning up etcd membership for %s...\n", nodeIP)

	client, err := p.createTalosClient(ctx, nodeIP)
	if err != nil {
		_, _ = fmt.Fprintf(p.logWriter,
			"  ⚠ Could not connect to %s for etcd cleanup: %v\n",
			nodeIP, err)

		return
	}

	defer client.Close() //nolint:errcheck

	// Step 1: Forfeit leadership if this node is the etcd leader.
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
		_, _ = fmt.Fprintf(p.logWriter,
			"  ⚠ Etcd leave failed on %s (best-effort): %v\n",
			nodeIP, err)
	} else {
		_, _ = fmt.Fprintf(p.logWriter,
			"  ✓ Etcd member removed from cluster (%s)\n", nodeIP)
	}
}
