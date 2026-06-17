package talosprovisioner

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/k8s/readiness"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// autoscalerRolloutTimeout bounds the wait for the restarted cluster-autoscaler
// Deployment to become ready before recycling nodes. Recycling drains nodes, which
// can make pods pending and trigger a scale-up; waiting first ensures that scale-up
// is served by the autoscaler pod that already carries the refreshed snapshot and
// worker config — not the pre-restart pod still holding the old template.
const autoscalerRolloutTimeout = 5 * time.Minute

// recycleAutoscalerNodes drains and deletes the cluster-autoscaler-managed nodes so
// the autoscaler re-provisions any still-needed capacity from the refreshed Talos
// snapshot + worker config. It is the disruptive propagation path, reserved for
// changes that can only reach an already-booted node by replacing it with a fresh
// server: a new Talos boot image (snapshot/OS bump) or a wipe/recreate-class change.
// A reboot-required change (CNI/disk-quota) instead reboots the same servers in
// place via rollingRebootAutoscalerNodes, and config-only drift that Talos applies
// with NO_REBOOT goes through applyInPlaceToAutoscalerNodes — neither needs a fresh
// server. See autoscalerRecycleRequired / autoscalerRebootRequired for the gate.
//
// Why this is needed: `cluster update` upgrades only KSail-owned nodes in place
// (control planes + static workers, listed via the ksail.owned label). Autoscaler
// nodes carry the hcloud/node-group label instead, so the rolling Talos/Kubernetes
// upgrade never touches them; left alone they keep their old versions until organic
// scale-down. Refreshing the Secret only fixes *newly* provisioned nodes.
//
// The design mirrors the upstream Cluster Autoscaler model: the autoscaler owns node
// *creation*, so KSail does not provision replacements here — it removes the stale
// nodes gracefully (cordon + drain via the eviction API, honoring PodDisruption
// Budgets) and lets the autoscaler bring up fresh nodes from the new template on
// demand. Compute-only autoscaler nodes hold no persistent storage and no etcd
// membership, so replace-by-recreation is the idiomatic, lossless path.
func (p *Provisioner) recycleAutoscalerNodes(ctx context.Context, clusterName string) error {
	servers, err := p.listAutoscalerServers(ctx, clusterName)
	if err != nil {
		return err
	}

	if len(servers) == 0 {
		_, _ = fmt.Fprintf(p.logWriter, "  ⓘ No autoscaler nodes to recycle\n")

		return nil
	}

	hzProvider, err := p.hetznerProvider()
	if err != nil {
		return err
	}

	clientset, err := p.createK8sClient(clusterName)
	if err != nil {
		return err
	}

	// Wait for the restarted autoscaler to be ready so any scale-up the drain
	// triggers is served from the new template, not the pre-restart pod.
	rolloutErr := p.waitForAutoscalerRollout(ctx, clientset)
	if rolloutErr != nil {
		return rolloutErr
	}

	return p.recycleAutoscalerServers(ctx, clientset, hzProvider, sortServersByName(servers))
}

// listAutoscalerServers returns the running autoscaler-managed servers for the
// cluster, or nil when the node autoscaler is disabled or no pools are configured.
// Both the recycle and in-place propagation paths enumerate nodes this way, so the
// disabled-guard and per-pool lookup live in one place.
func (p *Provisioner) listAutoscalerServers(
	ctx context.Context,
	clusterName string,
) ([]*hcloud.Server, error) {
	if p.hetznerOpts == nil || !p.hetznerOpts.NodeAutoscalerEnabled ||
		len(p.hetznerOpts.AutoscalerNodePoolNames) == 0 {
		return nil, nil
	}

	hzProvider, err := p.hetznerProvider()
	if err != nil {
		return nil, err
	}

	servers, err := hzProvider.ListAutoscalerNodes(
		ctx, clusterName, p.hetznerOpts.AutoscalerNodePoolNames,
	)
	if err != nil {
		return nil, fmt.Errorf("listing autoscaler nodes: %w", err)
	}

	return servers, nil
}

// applyInPlaceToAutoscalerNodes pushes the refreshed worker configuration to the
// running autoscaler nodes with a NO_REBOOT apply — the same non-disruptive path
// KSail-owned workers get for in-place changes. It is the counterpart to
// recycleAutoscalerNodes for config-only drift: because it never cordons or drains,
// a strict PodDisruptionBudget (e.g. a singleton with maxUnavailable: 0) can no
// longer stall the whole update the way a recycle's eviction loop can.
//
// Each node is reconciled independently via applyNodeConfig, which overlays the
// role-scoped worker patches onto the node's *running* config — preserving the
// create/boot-time settings an autoscaler node already carries (its server-name
// hostname, the autoscaled marker, pool labels/taints) while still landing the new
// patch. Per-node failures are recorded on result (surfacing as a failed update)
// rather than aborting the loop, so one unreachable node does not block the rest.
func (p *Provisioner) applyInPlaceToAutoscalerNodes(
	ctx context.Context,
	clusterName string,
	result *clusterupdate.UpdateResult,
) error {
	servers, err := p.listAutoscalerServers(ctx, clusterName)
	if err != nil {
		return err
	}

	if len(servers) == 0 {
		_, _ = fmt.Fprintf(p.logWriter, "  ⓘ No autoscaler nodes to reconcile\n")

		return nil
	}

	_, _ = fmt.Fprintf(p.logWriter,
		"Applying refreshed config to %d autoscaler node(s) in place (no reboot)...\n",
		len(servers))

	// buildDesiredNodeConfig rebuilds each node's config from its running config +
	// the cluster PKI, which only a control-plane node carries; seed it from one
	// up front and reuse it for every node (see fetchSecretsSource / #4963).
	secretsSource := p.fetchSecretsSource(ctx, clusterName)

	for _, server := range sortServersByName(servers) {
		serverIP, addrErr := hetznerNodeTalosAddress(server)
		if addrErr != nil {
			p.recordNodeConfigFailure(
				nodeWithRole{IP: server.Name, Role: RoleWorker}, result,
				fmt.Sprintf("resolve address: %v", addrErr),
			)

			continue
		}

		p.applyNodeConfig(ctx, nodeWithRole{IP: serverIP, Role: RoleWorker}, secretsSource, result)
	}

	return nil
}

// recycleAutoscalerServers recycles the given autoscaler servers one at a time,
// reporting progress. It stops at the first failure so a partial run leaves the
// remaining nodes untouched rather than draining the whole pool on a transient error.
func (p *Provisioner) recycleAutoscalerServers(
	ctx context.Context,
	clientset kubernetes.Interface,
	hzProvider *hetzner.Provider,
	ordered []*hcloud.Server,
) error {
	_, _ = fmt.Fprintf(p.logWriter,
		"Recycling %d autoscaler node(s) so they follow the new baseline...\n", len(ordered))

	for idx, server := range ordered {
		_, _ = fmt.Fprintf(p.logWriter,
			"  [%d/%d] Recycling autoscaler node %s...\n", idx+1, len(ordered), server.Name)

		recycleErr := p.recycleSingleAutoscalerNode(ctx, clientset, hzProvider, server)
		if recycleErr != nil {
			return fmt.Errorf("recycling autoscaler node %s: %w", server.Name, recycleErr)
		}

		_, _ = fmt.Fprintf(p.logWriter, "  ✓ Recycled autoscaler node %s\n", server.Name)
	}

	return nil
}

// recycleSingleAutoscalerNode performs the cordon → drain → delete sequence for a
// single autoscaler-managed server. It does not provision a replacement; the
// cluster-autoscaler does that from the refreshed template when workloads need it.
func (p *Provisioner) recycleSingleAutoscalerNode(
	ctx context.Context,
	clientset kubernetes.Interface,
	hzProvider *hetzner.Provider,
	server *hcloud.Server,
) error {
	serverIP, addrErr := hetznerNodeTalosAddress(server)
	if addrErr != nil {
		return fmt.Errorf("resolving address for %s: %w", server.Name, addrErr)
	}

	// 1. Cordon and drain the outgoing node before removing it.
	nodeName, drainErr := p.drainResolvedNode(ctx, clientset, serverIP)
	if drainErr != nil {
		return drainErr
	}

	// 2. Delete the outgoing Hetzner server.
	deleteErr := p.deleteHetznerServer(ctx, hzProvider, server)
	if deleteErr != nil {
		return deleteErr
	}

	// 3. Remove the now-stale Kubernetes node object (best-effort).
	if nodeName != "" {
		p.deleteK8sNode(ctx, clientset, nodeName)
	}

	return nil
}

// waitForAutoscalerRollout blocks until every cluster-autoscaler Deployment matching
// the standard instance label is ready, so a freshly restarted autoscaler is serving
// requests before nodes are recycled. A missing Deployment is not an error — the
// autoscaler may not be installed, in which case there is nothing to wait for.
func (p *Provisioner) waitForAutoscalerRollout(
	ctx context.Context,
	clientset kubernetes.Interface,
) error {
	deployments, err := clientset.AppsV1().
		Deployments(autoscalerConfigSecretNamespace).
		List(ctx, metav1.ListOptions{LabelSelector: autoscalerDeploymentSelector})
	if err != nil {
		return fmt.Errorf("listing cluster-autoscaler deployment: %w", err)
	}

	for index := range deployments.Items {
		name := deployments.Items[index].Name

		readyErr := readiness.WaitForDeploymentReady(
			ctx, clientset, autoscalerConfigSecretNamespace, name, autoscalerRolloutTimeout,
		)
		if readyErr != nil {
			return fmt.Errorf("waiting for cluster-autoscaler %s rollout: %w", name, readyErr)
		}
	}

	return nil
}

// sortServersByName returns the servers ordered by name for deterministic,
// one-at-a-time recycling.
func sortServersByName(servers []*hcloud.Server) []*hcloud.Server {
	ordered := slices.Clone(servers)
	slices.SortFunc(ordered, func(a, b *hcloud.Server) int {
		return strings.Compare(a.Name, b.Name)
	})

	return ordered
}
