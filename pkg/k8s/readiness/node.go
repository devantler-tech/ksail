package readiness

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// WaitForNodeReady polls until at least one node has condition Ready=True.
// This is used after CNI installation to ensure the network layer is functional
// before proceeding to install post-CNI components.
func WaitForNodeReady(
	ctx context.Context,
	clientset kubernetes.Interface,
	deadline time.Duration,
) error {
	return waitForNodes(ctx, clientset, deadline, func(nodes []corev1.Node) bool {
		for i := range nodes {
			if isNodeReady(&nodes[i]) {
				return true
			}
		}

		return false
	})
}

// WaitForAllNodesReady polls until every node in the cluster has condition Ready=True.
// Unlike WaitForNodeReady, which succeeds when at least one node is Ready, this function
// waits for all listed nodes to report Ready=True before proceeding. This helps avoid
// moving on while nodes still have transient NotReady state during early cluster
// initialization, but it does not verify schedulability, taints, cordon state
// (spec.unschedulable), or other scheduling constraints.
func WaitForAllNodesReady(
	ctx context.Context,
	clientset kubernetes.Interface,
	deadline time.Duration,
) error {
	return waitForNodes(ctx, clientset, deadline, allNodesReady)
}

// allNodesReady returns true when the node list is non-empty and every node
// has condition Ready=True. It is used as a building block by both
// WaitForAllNodesReady and WaitForAllNodesReadyAndSchedulable.
func allNodesReady(nodes []corev1.Node) bool {
	if len(nodes) == 0 {
		return false
	}

	for i := range nodes {
		if !isNodeReady(&nodes[i]) {
			return false
		}
	}

	return true
}

// ListNodesOrContinue lists cluster nodes for use inside a PollForReadiness check func: a transient
// list error returns (nil, nil) rather than failing the poll, so the caller's check simply sees no
// nodes this tick and the polling loop continues instead of aborting on a passing API hiccup.
//
// The error return is intentionally always nil today (never actionable) — every failure path is
// swallowed into the (nil, nil) "continue polling" case above. It is kept in the signature, rather
// than dropped, so a future change to add a real terminal-error case (e.g. a non-transient
// permission failure that should abort the poll instead of retrying) doesn't need an API change;
// callers must not assume a non-nil error can occur today.
func ListNodesOrContinue(
	ctx context.Context,
	clientset kubernetes.Interface,
) ([]corev1.Node, error) {
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, nil //nolint:nilerr // returning nil to continue polling
	}

	return nodes.Items, nil
}

// waitForNodes polls nodes and passes them to the check function until it returns true.
func waitForNodes(
	ctx context.Context,
	clientset kubernetes.Interface,
	deadline time.Duration,
	check func([]corev1.Node) bool,
) error {
	return PollForReadiness(ctx, deadline, func(ctx context.Context) (bool, error) {
		nodes, err := ListNodesOrContinue(ctx, clientset)
		if err != nil {
			return false, err
		}

		return check(nodes), nil
	})
}

// WaitForAllNodesLabeled polls until every node in the cluster has the given
// label key set (any non-empty value is accepted). This is used to gate
// components that depend on labels applied asynchronously by an external
// controller — for example, the Hetzner cloud controller manager applies
// `instance.hetzner.cloud/provided-by` after initializing each node, and the
// Hetzner CSI node driver must wait for that label before registering topology
// keys with the kubelet CSI plugin.
func WaitForAllNodesLabeled(
	ctx context.Context,
	clientset kubernetes.Interface,
	labelKey string,
	deadline time.Duration,
) error {
	return waitForNodes(ctx, clientset, deadline, func(nodes []corev1.Node) bool {
		if len(nodes) == 0 {
			return false
		}

		for i := range nodes {
			if nodes[i].Labels[labelKey] == "" {
				return false
			}
		}

		return true
	})
}

// WaitForAllNodesReadyAndSchedulable polls until every node in the cluster has
// condition Ready=True and at least one node is schedulable (not cordoned, and
// carries no NoSchedule or NoExecute taints). This prevents deploying workloads
// before the control-plane taint has been removed on single-node clusters,
// avoiding the FailedScheduling race condition where Kind marks a node Ready
// but the node-role.kubernetes.io/control-plane:NoSchedule taint still lingers.
func WaitForAllNodesReadyAndSchedulable(
	ctx context.Context,
	clientset kubernetes.Interface,
	deadline time.Duration,
) error {
	return WaitForAllNodesReadyAndSchedulableIgnoringTaints(ctx, clientset, deadline)
}

// TaintExternalCloudProviderUninitialized is the well-known taint key applied by
// kubelet when started with --cloud-provider=external. The external cloud
// controller manager (CCM) removes this taint after initializing the node with
// a providerID and cloud-specific labels. During CNI readiness checks this taint
// should be tolerated because it is an expected transient state, not a CNI failure.
const TaintExternalCloudProviderUninitialized = "node.cloudprovider.kubernetes.io/uninitialized"

// WaitForAllNodesReadyAndSchedulableIgnoringTaints is like
// WaitForAllNodesReadyAndSchedulable but tolerates taints whose keys appear in
// ignoredTaintKeys. This is used during CNI readiness checks on clusters with
// an external cloud provider, where nodes carry the
// node.cloudprovider.kubernetes.io/uninitialized taint until the CCM is
// installed (which happens after CNI installation).
func WaitForAllNodesReadyAndSchedulableIgnoringTaints(
	ctx context.Context,
	clientset kubernetes.Interface,
	deadline time.Duration,
	ignoredTaintKeys ...string,
) error {
	ignored := make(map[string]struct{}, len(ignoredTaintKeys))
	for _, k := range ignoredTaintKeys {
		ignored[k] = struct{}{}
	}

	return waitForNodes(ctx, clientset, deadline, func(nodes []corev1.Node) bool {
		if !allNodesReady(nodes) {
			return false
		}

		for i := range nodes {
			if isNodeSchedulableIgnoringTaints(&nodes[i], ignored) {
				return true
			}
		}

		return false
	})
}

// isNodeReady returns true if the node has condition Ready=True.
func isNodeReady(node *corev1.Node) bool {
	for _, cond := range node.Status.Conditions {
		if cond.Type == corev1.NodeReady {
			return cond.Status == corev1.ConditionTrue
		}
	}

	return false
}

// isNodeSchedulableIgnoringTaints returns true if workload pods without any
// tolerations can be scheduled onto the node, skipping taints whose keys are in
// the ignored set. A node is considered unschedulable when any of the following
// conditions hold:
//   - The node is cordoned (spec.unschedulable=true)
//   - The node carries a NoSchedule taint (scheduler rejects pods without a
//     matching toleration) that is not in the ignored set
//   - The node carries a NoExecute taint (scheduler rejects pods without a
//     matching toleration — eviction of already-running pods is a separate
//     concern, but new pods are not admitted either) that is not in the ignored
//     set
func isNodeSchedulableIgnoringTaints(node *corev1.Node, ignored map[string]struct{}) bool {
	if node.Spec.Unschedulable {
		return false
	}

	for _, taint := range node.Spec.Taints {
		if _, ok := ignored[taint.Key]; ok {
			continue
		}

		if taint.Effect == corev1.TaintEffectNoSchedule ||
			taint.Effect == corev1.TaintEffectNoExecute {
			return false
		}
	}

	return true
}
