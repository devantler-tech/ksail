package talosprovisioner

import (
	"context"
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"k8s.io/client-go/kubernetes"
)

// rollingRebootAutoscalerNodes brings existing autoscaler nodes to the refreshed
// baseline by rebooting each one IN PLACE — cordon → drain → stage the desired
// config (STAGED) → reboot the same Hetzner server → wait Ready → uncordon — the
// same mechanism KSail-owned static nodes use for reboot-required changes
// (rollingApplyRebootChanges). It is the propagation path for a reboot-required
// change (CNI swap, disk-quota toggle) that a NO_REBOOT apply cannot land: unlike
// recycleAutoscalerNodes it never deletes a server, so a capacity-constrained
// project at its Hetzner server limit still converges (the #5219 incident) — no
// fresh server is ever needed.
//
// The reboot-required classification is cluster-wide (detectDisruptiveConfigChanges
// compares one control-plane node; the CNI/disk-quota patches apply identically to
// workers), so this does not diff each autoscaler worker individually. A worker
// already carrying the change just stages an equivalent config and reboots
// harmlessly. Each node's config is rebuilt from its RUNNING config via
// buildStagedNodeConfig → buildDesiredNodeConfig, preserving the per-node sections
// an autoscaler node carries (server-name hostname, the ksail.io/autoscaled marker,
// pool labels/taints) — see applyInPlaceToAutoscalerNodes.
//
// Draining a node can make pods pending and briefly prompt the autoscaler to add a
// node, which it scales back down once the rebooted node returns; nodes are handled
// one at a time to bound that transient — still strictly cheaper than recycle's hard
// requirement of spare server headroom. waitForAutoscalerRollout first ensures any
// such scale-up is served by the autoscaler pod already carrying the refreshed
// template (the Secret changed and the Deployment was restarted upstream). The PKI
// the staged-config rebuild needs is present because needsSecretSync forces a
// secrets sync whenever the autoscaler is enabled, ahead of this step.
func (p *Provisioner) rollingRebootAutoscalerNodes(
	ctx context.Context,
	clusterName string,
	result *clusterupdate.UpdateResult,
) error {
	clientset, ordered, ok, err := p.prepareAutoscalerNodeConvergence(
		ctx, clusterName, "  ⓘ No autoscaler nodes to reboot\n",
	)
	if err != nil || !ok {
		return err
	}

	// buildStagedNodeConfig rebuilds each node's config from its running config + the
	// cluster PKI, which only a control-plane node carries; seed it once and reuse it
	// for every node (see fetchSecretsSource / #4963). A worker source would fail
	// "parse PEM block".
	secretsSource := p.fetchSecretsSource(ctx, clusterName)

	return p.rollingRebootAutoscalerServers(ctx, clientset, ordered, secretsSource, result)
}

// rollingRebootAutoscalerServers reboots the given autoscaler servers in place, one
// at a time, recording progress. A server with no usable address, or any node whose
// reboot fails, aborts the run so a partial reboot leaves the remaining nodes
// untouched — except a server present in Hetzner but not (yet) registered in
// Kubernetes, which is skipped: it cannot be cordoned/drained, and wedging the whole
// convergence on one half-joined node is the #5219 failure mode this avoids (mirrors
// the recycle path's drainResolvedNode tolerance).
func (p *Provisioner) rollingRebootAutoscalerServers(
	ctx context.Context,
	clientset kubernetes.Interface,
	ordered []*hcloud.Server,
	secretsSource talosconfig.Provider,
	result *clusterupdate.UpdateResult,
) error {
	_, _ = fmt.Fprintf(p.logWriter,
		"Rolling reboot of %d autoscaler node(s) in place (same servers)...\n", len(ordered))

	for idx, server := range ordered {
		serverIP, addrErr := hetznerNodeTalosAddress(server)
		if addrErr != nil {
			// No usable private address is a real misconfiguration, not a transient
			// half-joined node: abort rather than silently skip (a skipped node stays
			// stale and re-creates the stale-node-pins-project condition this fixes).
			recordFailedChange(result, RoleWorker, server.Name, addrErr)

			return fmt.Errorf("resolving address for autoscaler node %s: %w", server.Name, addrErr)
		}

		_, _ = fmt.Fprintf(
			p.logWriter,
			"  [%d/%d] Rolling reboot for autoscaler node %s...\n",
			idx+1, len(ordered), server.Name,
		)

		rebootErr := p.rollingRebootSingleNode(
			ctx, clientset, nodeWithRole{IP: serverIP, Role: RoleWorker}, secretsSource,
		)

		switch {
		case rebootErr == nil:
			result.RebootsPerformed++

			recordAppliedChange(result, RoleWorker, server.Name, "rebooted")

			_, _ = fmt.Fprintf(p.logWriter, "  ✓ Rebooted autoscaler node %s\n", server.Name)
		case errors.Is(rebootErr, ErrNodeNotFoundByIP):
			// Present in Hetzner but not (yet) joined: it can't be drained, so skip it
			// instead of wedging the whole convergence (mirrors drainResolvedNode).
			_, _ = fmt.Fprintf(p.logWriter,
				"  ⚠ Autoscaler node %s is not registered in Kubernetes; skipping reboot\n",
				server.Name)
		default:
			recordFailedChange(result, RoleWorker, server.Name, rebootErr)

			return fmt.Errorf("rolling reboot autoscaler node %s: %w", server.Name, rebootErr)
		}
	}

	return nil
}
