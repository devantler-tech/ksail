package talosprovisioner

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/k8s/readiness"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ErrFloatingIPReconcileBeforeControlPlaneRoll prevents a combined endpoint
// enablement/drift repair and destructive control-plane replacement. The
// endpoint must be established in a separate update before its current direct
// control-plane address can safely be replaced.
var ErrFloatingIPReconcileBeforeControlPlaneRoll = errors.New(
	"floating IP endpoint must be reconciled before a control-plane rolling recreate; " +
		"apply floatingIPEnabled without server-type changes, then retry",
)

// applyRollingRecreateChanges replaces Hetzner nodes one at a time to apply a
// server-type change recorded in result.RollingRecreate. Workers are replaced
// before control planes to minimise control-plane disruption. Each replacement
// drains and removes the outgoing node (with etcd membership cleanup for control
// planes), provisions a server with the new type, applies config so it rejoins
// the cluster, and waits for it to become Ready before moving on — preserving
// etcd quorum throughout.
func (p *Provisioner) applyRollingRecreateChanges(
	ctx context.Context,
	clusterName string,
	result *clusterupdate.UpdateResult,
) error {
	if len(result.RollingRecreate) == 0 {
		return nil
	}

	// Rolling node replacement is only implemented for the Hetzner provider.
	// Other providers never classify server-type changes as rolling-recreate.
	if p.hetznerOpts == nil || p.infraProvider == nil {
		return nil
	}

	validateErr := p.validateUpdatePlan(result)
	if validateErr != nil {
		return validateErr
	}

	rollControlPlane, rollWorker := rolesFromRollingChanges(result.RollingRecreate)

	clientset, err := p.createK8sClient(clusterName)
	if err != nil {
		return err
	}

	return p.rollRolesWorkerFirst(ctx, clientset, clusterName, rollWorker, rollControlPlane, result)
}

// rollRolesWorkerFirst replaces the requested roles' nodes, workers before
// control planes to minimise control-plane disruption.
func (p *Provisioner) rollRolesWorkerFirst(
	ctx context.Context,
	clientset kubernetes.Interface,
	clusterName string,
	rollWorker, rollControlPlane bool,
	result *clusterupdate.UpdateResult,
) error {
	if rollWorker {
		workerErr := p.rollingReplaceRole(ctx, clientset, clusterName, RoleWorker, result)
		if workerErr != nil {
			return workerErr
		}
	}

	if rollControlPlane {
		cpErr := p.rollingReplaceRole(ctx, clientset, clusterName, RoleControlPlane, result)
		if cpErr != nil {
			return cpErr
		}
	}

	return nil
}

// rolesFromRollingChanges reports which roles have a rolling server-type change.
func rolesFromRollingChanges(changes []clusterupdate.Change) (bool, bool) {
	var controlPlane, worker bool

	for _, change := range changes {
		switch change.Field {
		case "provider.hetzner.controlPlaneServerType":
			controlPlane = true
		case "provider.hetzner.workerServerType":
			worker = true
		}
	}

	return controlPlane, worker
}

// rollingReplaceRole replaces every node of the given role whose server type
// differs from the desired type, one at a time.
func (p *Provisioner) rollingReplaceRole(
	ctx context.Context,
	clientset kubernetes.Interface,
	clusterName, role string,
	result *clusterupdate.UpdateResult,
) error {
	hzProvider, existing, err := p.hetznerNodesForRole(ctx, clusterName, role)
	if err != nil {
		return err
	}

	desiredType := p.hetznerServerType(role)

	toReplace := serversNeedingReplacement(existing, desiredType)
	if len(toReplace) == 0 {
		return nil
	}

	// Fail fast before mutating infrastructure if the new type is unavailable.
	availErr := p.checkHetznerAvailabilityForRole(ctx, hzProvider, role)
	if availErr != nil {
		return availErr
	}

	infra, infraErr := p.ensureHetznerInfra(ctx, hzProvider, clusterName)
	if infraErr != nil {
		return fmt.Errorf("failed to ensure Hetzner infrastructure: %w", infraErr)
	}

	for idx, oldServer := range toReplace {
		// Re-validate etcd-quorum redundancy against the live inventory before
		// each deletion. A prior iteration, a concurrent scale-down, or a node
		// failure could have reduced the count since the loop began.
		guardErr := p.guardControlPlaneQuorum(ctx, hzProvider, clusterName, role)
		if guardErr != nil {
			return guardErr
		}

		_, _ = fmt.Fprintf(p.logWriter,
			"  [%d/%d] Replacing %s server %s with type %s...\n",
			idx+1, len(toReplace), role, oldServer.Name, desiredType)

		replaceErr := p.rollingReplaceSingleNode(
			ctx, clientset, hzProvider, clusterName, role, oldServer, infra,
		)
		if replaceErr != nil {
			recordFailedChange(result, role, oldServer.Name, replaceErr)

			return fmt.Errorf("failed to replace %s server %s: %w",
				role, oldServer.Name, replaceErr)
		}

		recordAppliedChange(result, role, oldServer.Name, "replaced")

		_, _ = fmt.Fprintf(p.logWriter, "  ✓ Replaced %s server %s\n", role, oldServer.Name)
	}

	return nil
}

// guardControlPlaneQuorum refuses to proceed with a control-plane replacement
// unless at least MinControlPlanesForRollingReplace control planes are currently
// present, preserving etcd quorum when the outgoing node is deleted. It is a
// no-op for non-control-plane roles.
func (p *Provisioner) guardControlPlaneQuorum(
	ctx context.Context,
	hzProvider *hetzner.Provider,
	clusterName, role string,
) error {
	if role != RoleControlPlane {
		return nil
	}

	current, listErr := p.listHetznerNodesByRole(ctx, hzProvider, clusterName, role)
	if listErr != nil {
		return fmt.Errorf("failed to re-list %s nodes for quorum check: %w", role, listErr)
	}

	if len(current) < clusterupdate.MinControlPlanesForRollingReplace {
		return fmt.Errorf("%w: %d present, need at least %d",
			ErrInsufficientControlPlanesForRoll,
			len(current), clusterupdate.MinControlPlanesForRollingReplace)
	}

	return nil
}

// serversNeedingReplacement returns servers whose type differs from desiredType.
func serversNeedingReplacement(
	servers []*hcloud.Server,
	desiredType string,
) []*hcloud.Server {
	out := make([]*hcloud.Server, 0, len(servers))

	for _, server := range servers {
		if server == nil {
			continue
		}

		if server.ServerType == nil || !strings.EqualFold(server.ServerType.Name, desiredType) {
			out = append(out, server)
		}
	}

	return out
}

// rollingReplaceSingleNode performs the drain → remove → recreate → rejoin
// sequence for a single node, blocking until the replacement is Ready.
func (p *Provisioner) rollingReplaceSingleNode(
	ctx context.Context,
	clientset kubernetes.Interface,
	hzProvider *hetzner.Provider,
	clusterName, role string,
	oldServer *hcloud.Server,
	infra HetznerInfra,
) error {
	oldIP, addrErr := hetznerNodeTalosAddress(oldServer)
	if addrErr != nil {
		return fmt.Errorf("resolving address for %s: %w", oldServer.Name, addrErr)
	}

	// 1. Cordon and drain the outgoing node before removing it.
	oldNodeName, drainErr := p.drainResolvedNode(ctx, clientset, oldIP)
	if drainErr != nil {
		return drainErr
	}

	// 2. Control-plane etcd membership cleanup before removal.
	if role == RoleControlPlane {
		p.etcdCleanupBeforeRemoval(ctx, oldIP)
	}

	// 3. Delete the outgoing Hetzner server.
	deleteErr := p.deleteHetznerServer(ctx, hzProvider, oldServer)
	if deleteErr != nil {
		return deleteErr
	}

	// 4. Remove the now-stale Kubernetes node object (best-effort).
	if oldNodeName != "" {
		p.deleteK8sNode(ctx, clientset, oldNodeName)
	}

	// 5. Provision the replacement server with the new type and let it rejoin.
	newServer, createErr := p.createReplacementServer(ctx, hzProvider, clusterName, role, infra)
	if createErr != nil {
		return createErr
	}

	return p.configureAndWaitReplacement(
		ctx, clientset, hzProvider, clusterName, oldServer, newServer, role,
	)
}

// reattachFloatingIPAfterControlPlaneReplacement restores the stable API
// endpoint when deleting its former control-plane server left the configured
// floating IP unassigned. A surviving control plane may already have claimed
// the IP through Talos VIP election; in that case the assignment is preserved
// instead of being moved unnecessarily. This lookup is deliberately read-only:
// endpoint enablement drift remains the later reconciliation step's job.
func (p *Provisioner) reattachFloatingIPAfterControlPlaneReplacement(
	ctx context.Context,
	hzProvider *hetzner.Provider,
	clusterName string,
	oldServer, newServer *hcloud.Server,
) error {
	if p.hetznerOpts == nil || !p.hetznerOpts.FloatingIPEnabled {
		return nil
	}

	floatingIP, lookupErr := hzProvider.GetOwnedFloatingIP(ctx, clusterName)
	if lookupErr != nil {
		return fmt.Errorf("looking up floating IP after control-plane replacement: %w", lookupErr)
	}

	if floatingIP == nil {
		return nil
	}

	if floatingIP.Server != nil && floatingIP.Server.ID != oldServer.ID {
		return nil
	}

	attachErr := hzProvider.AttachFloatingIPToServer(ctx, floatingIP, newServer)
	if attachErr != nil {
		return fmt.Errorf(
			"reattaching floating IP to replacement %s: %w", newServer.Name, attachErr,
		)
	}

	return nil
}

// finishControlPlaneReplacement retries a failed endpoint reattachment before
// the endpoint-dependent Kubernetes Ready gate, then always runs that gate and
// reports either or both final failures.
func finishControlPlaneReplacement(reattach, waitReady func() error) error {
	reattachErr := reattach()
	if reattachErr != nil {
		reattachErr = reattach()
	}

	return errors.Join(reattachErr, waitReady())
}

// configureAndWaitReplacement waits for the Talos API on a freshly provisioned
// replacement server, applies its role config so it rejoins the cluster,
// restores a displaced floating-IP endpoint after the replacement is reachable,
// then blocks until it has installed, rebooted, and become Ready in Kubernetes.
func (p *Provisioner) configureAndWaitReplacement(
	ctx context.Context,
	clientset kubernetes.Interface,
	hzProvider *hetzner.Provider,
	clusterName string,
	oldServer, newServer *hcloud.Server,
	role string,
) error {
	prepareErr := p.prepareFloatingIPConfigForNewControlPlane(
		ctx, hzProvider, clusterName, role,
	)
	if prepareErr != nil {
		return prepareErr
	}

	configErr := p.applyConfigToReplacement(ctx, newServer, role)
	if configErr != nil {
		return configErr
	}

	waitReady := func() error {
		_, _ = fmt.Fprintf(p.logWriter,
			"    Waiting for replacement %s to rejoin and become Ready...\n", newServer.Name)

		readyErr := p.waitForReplacementNodeReady(ctx, clientset, newServer)
		if readyErr != nil {
			return fmt.Errorf("waiting for replacement %s to become Ready: %w",
				newServer.Name, readyErr)
		}

		return nil
	}

	if role != RoleControlPlane {
		return waitReady()
	}

	reattach := func() error {
		reattachErr := p.reattachFloatingIPAfterControlPlaneReplacement(
			ctx, hzProvider, clusterName, oldServer, newServer,
		)
		if reattachErr != nil {
			_, _ = fmt.Fprintf(p.logWriter,
				"    ⚠ Floating IP reattachment failed; verifying replacement readiness: %v\n",
				reattachErr)
		}

		return reattachErr
	}

	return finishControlPlaneReplacement(reattach, waitReady)
}

// applyConfigToReplacement waits for the Talos API on a freshly provisioned
// replacement server, applies its role config so it rejoins the cluster, and
// waits for the server to become reachable again afterwards.
func (p *Provisioner) applyConfigToReplacement(
	ctx context.Context,
	newServer *hcloud.Server,
	role string,
) error {
	servers := []*hcloud.Server{newServer}

	_, _ = fmt.Fprintf(p.logWriter,
		"    Waiting for Talos API on replacement %s...\n", newServer.Name)

	apiErr := p.waitForHetznerTalosAPI(ctx, servers)
	if apiErr != nil {
		return fmt.Errorf("waiting for Talos API on replacement %s: %w", newServer.Name, apiErr)
	}

	config := p.configForRole(role)
	if config == nil {
		return fmt.Errorf("%w: %s", ErrNoConfigForRole, role)
	}

	applyErr := p.applyConfigToNode(ctx, newServer, config)
	if applyErr != nil {
		return fmt.Errorf("applying config to replacement %s: %w", newServer.Name, applyErr)
	}

	reachErr := p.waitForServersToBeReachable(ctx, servers)
	if reachErr != nil {
		return fmt.Errorf("waiting for replacement %s to become reachable: %w",
			newServer.Name, reachErr)
	}

	return nil
}

// drainResolvedNode cordons and drains the Kubernetes node backing the given Talos
// node IP, returning its resolved node name. It returns "" with no error when the
// node is not registered in Kubernetes — e.g. already removed by a prior run or
// scaled down by the autoscaler — so the caller can still remove the underlying
// server. A node whose lookup fails for any other reason (a Kubernetes API error,
// not a missing node) is returned as an error so callers fail closed rather than
// deleting an undrained node and abruptly evicting its workloads.
func (p *Provisioner) drainResolvedNode(
	ctx context.Context,
	clientset kubernetes.Interface,
	nodeIP string,
) (string, error) {
	nodeName, resolveErr := p.resolveNodeName(ctx, clientset, nodeIP)

	switch {
	case resolveErr == nil:
		drainErr := p.cordonAndDrain(ctx, clientset, nodeName)
		if drainErr != nil {
			return "", drainErr
		}

		return nodeName, nil
	case errors.Is(resolveErr, ErrNodeNotFoundByIP):
		_, _ = fmt.Fprintf(p.logWriter,
			"    ⚠ %s not registered in Kubernetes; proceeding with removal\n", nodeIP)

		return "", nil
	default:
		return "", fmt.Errorf("resolve Kubernetes node for %s: %w", nodeIP, resolveErr)
	}
}

// cordonAndDrain cordons then drains the named Kubernetes node.
func (p *Provisioner) cordonAndDrain(
	ctx context.Context,
	clientset kubernetes.Interface,
	nodeName string,
) error {
	_, _ = fmt.Fprintf(p.logWriter, "    Cordoning %s...\n", nodeName)

	cordonErr := p.cordonNode(ctx, clientset, nodeName)
	if cordonErr != nil {
		return fmt.Errorf("cordon: %w", cordonErr)
	}

	_, _ = fmt.Fprintf(p.logWriter, "    Draining %s...\n", nodeName)

	drainErr := p.drainNode(ctx, clientset, nodeName)
	if drainErr != nil {
		// The drain runs before the node is removed, so a failure aborts the update
		// with the node still cordoned. Uncordon it (best-effort) so the cluster
		// returns to its prior schedulable state instead of permanently losing the
		// node's capacity. Detach from the parent ctx (which may already be cancelled,
		// e.g. Ctrl-C) but bound the cleanup so an unreachable API can't hang it.
		cleanupCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx), uncordonCleanupTimeout,
		)
		defer cancel()

		uncordonErr := p.uncordonNode(cleanupCtx, clientset, nodeName)
		if uncordonErr != nil {
			_, _ = fmt.Fprintf(p.logWriter,
				"    ⚠ Failed to uncordon %s after drain failure (best-effort): %v\n",
				nodeName, uncordonErr)
		}

		return fmt.Errorf("drain: %w", drainErr)
	}

	return nil
}

// createReplacementServer provisions a single new server for the role. The index
// is computed after the outgoing server was deleted, so its freed slot is the
// lowest available index and the replacement reclaims the removed node's name
// rather than allocating a higher one (#5312).
func (p *Provisioner) createReplacementServer(
	ctx context.Context,
	hzProvider *hetzner.Provider,
	clusterName, role string,
	infra HetznerInfra,
) (*hcloud.Server, error) {
	existing, listErr := p.listHetznerNodesByRole(ctx, hzProvider, clusterName, role)
	if listErr != nil {
		return nil, fmt.Errorf("failed to list %s nodes: %w", role, listErr)
	}

	indices := availableHetznerNodeIndices(existing, clusterName, role, 1)

	// Boot the replacement from the cluster's Talos snapshot image (when configured)
	// rather than the maintenance-mode ISO, so it runs the same Talos version as the
	// node it replaces and can parse the cluster's machine config (see hetznerBootSource).
	imageID, snapErr := p.ensureSnapshotImage(ctx, clusterName)
	if snapErr != nil {
		return nil, snapErr
	}

	creationResults, createErr := p.launchHetznerScaleCreation(
		ctx, hzProvider, clusterName, role, infra, p.hetznerRetryOpts(), indices, imageID,
	)
	if createErr != nil {
		return nil, createErr
	}

	servers, collectErr := p.collectCreatedHetznerServers(creationResults, role)
	if collectErr != nil {
		return nil, collectErr
	}

	if len(servers) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrReplacementServerNotCreated, role)
	}

	return servers[0], nil
}

// deleteK8sNode removes a stale Kubernetes node object on a best-effort basis.
// A missing node is treated as success; other failures are logged but not fatal,
// since the Talos/etcd-level removal has already happened.
func (p *Provisioner) deleteK8sNode(
	ctx context.Context,
	clientset kubernetes.Interface,
	nodeName string,
) {
	err := clientset.CoreV1().Nodes().Delete(ctx, nodeName, metav1.DeleteOptions{})

	switch {
	case err == nil:
		_, _ = fmt.Fprintf(p.logWriter, "    ✓ Removed stale node object %s\n", nodeName)
	case apierrors.IsNotFound(err):
		_, _ = fmt.Fprintf(p.logWriter, "    ✓ Node object %s already removed\n", nodeName)
	default:
		_, _ = fmt.Fprintf(p.logWriter,
			"    ⚠ Failed to remove stale node object %s (best-effort): %v\n", nodeName, err)
	}
}

// waitForReplacementNodeReady polls until the Kubernetes node backing the new
// server reports Ready=True. The node is matched by name (Talos derives the
// hostname from the Hetzner server name) or by its public IP.
func (p *Provisioner) waitForReplacementNodeReady(
	ctx context.Context,
	clientset kubernetes.Interface,
	server *hcloud.Server,
) error {
	serverIP, addrErr := hetznerNodeTalosAddress(server)
	if addrErr != nil {
		return fmt.Errorf("resolving address for %s: %w", server.Name, addrErr)
	}

	pollErr := readiness.PollForReadiness(
		ctx,
		nodeReadinessTimeout,
		func(ctx context.Context) (bool, error) {
			nodes, err := readiness.ListNodesOrContinue(ctx, clientset)
			if err != nil {
				return false, err //nolint:wrapcheck // ListNodesOrContinue never returns non-nil today; kept defensively
			}

			for i := range nodes {
				node := &nodes[i]
				if !nodeMatchesServer(node, server.Name, serverIP) {
					continue
				}

				return nodeIsReady(node), nil
			}

			return false, nil
		},
	)
	if pollErr != nil {
		return fmt.Errorf("wait for replacement node %s readiness: %w", server.Name, pollErr)
	}

	return nil
}

// nodeMatchesServer reports whether a Kubernetes node corresponds to the given
// Hetzner server, matching on node name (case-insensitive) or public IP address.
func nodeMatchesServer(node *corev1.Node, serverName, serverIP string) bool {
	if strings.EqualFold(node.Name, serverName) {
		return true
	}

	for _, addr := range node.Status.Addresses {
		if (addr.Type == corev1.NodeInternalIP || addr.Type == corev1.NodeExternalIP) &&
			addr.Address == serverIP {
			return true
		}
	}

	return false
}

// nodeIsReady reports whether a node has condition Ready=True.
func nodeIsReady(node *corev1.Node) bool {
	for _, cond := range node.Status.Conditions {
		if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}

	return false
}
