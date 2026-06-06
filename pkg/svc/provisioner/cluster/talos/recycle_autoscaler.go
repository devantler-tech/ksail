package talosprovisioner

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/k8s/readiness"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
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
// snapshot + worker config. It is invoked only after the cluster-autoscaler-config
// Secret actually changed (a Talos/Kubernetes version bump), so existing autoscaler
// nodes follow the new baseline instead of drifting on the old versions.
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
	if p.hetznerOpts == nil || !p.hetznerOpts.NodeAutoscalerEnabled ||
		len(p.hetznerOpts.AutoscalerNodePoolNames) == 0 {
		return nil
	}

	hzProvider, err := p.hetznerProvider()
	if err != nil {
		return err
	}

	servers, err := hzProvider.ListAutoscalerNodes(
		ctx, clusterName, p.hetznerOpts.AutoscalerNodePoolNames,
	)
	if err != nil {
		return fmt.Errorf("listing autoscaler nodes to recycle: %w", err)
	}

	if len(servers) == 0 {
		_, _ = fmt.Fprintf(p.logWriter, "  ⓘ No autoscaler nodes to recycle\n")

		return nil
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
	nodeName, resolveErr := p.resolveNodeName(ctx, clientset, serverIP)

	switch {
	case resolveErr == nil:
		drainErr := p.cordonAndDrain(ctx, clientset, nodeName)
		if drainErr != nil {
			return drainErr
		}
	case errors.Is(resolveErr, ErrNodeNotFoundByIP):
		// The server is no longer registered in Kubernetes (e.g. the autoscaler
		// already scaled it down, or a prior partial run removed it). Skip drain
		// and proceed with removal.
		_, _ = fmt.Fprintf(p.logWriter,
			"    ⚠ %s not registered in Kubernetes; proceeding with removal\n", serverIP)
	default:
		// A Kubernetes API failure (not a missing node): the node likely still
		// exists, so abort rather than deleting it undrained and evicting its
		// workloads abruptly.
		return fmt.Errorf("resolve Kubernetes node for %s: %w", serverIP, resolveErr)
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
