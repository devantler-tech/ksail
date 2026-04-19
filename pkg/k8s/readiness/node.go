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

// waitForNodes polls nodes and passes them to the check function until it returns true.
func waitForNodes(
	ctx context.Context,
	clientset kubernetes.Interface,
	deadline time.Duration,
	check func([]corev1.Node) bool,
) error {
	return PollForReadiness(ctx, deadline, func(ctx context.Context) (bool, error) {
		nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			return false, nil //nolint:nilerr // returning nil to continue polling
		}

		return check(nodes.Items), nil
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
	return waitForNodes(ctx, clientset, deadline, func(nodes []corev1.Node) bool {
		if !allNodesReady(nodes) {
			return false
		}

		for i := range nodes {
			if isNodeSchedulable(&nodes[i]) {
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

// isNodeSchedulable returns true if workload pods without any tolerations can
// be scheduled onto the node. A node is considered unschedulable when any of
// the following conditions hold:
//   - The node is cordoned (spec.unschedulable=true)
//   - The node carries a NoSchedule taint (scheduler rejects pods without a
//     matching toleration)
//   - The node carries a NoExecute taint (scheduler rejects pods without a
//     matching toleration — eviction of already-running pods is a separate
//     concern, but new pods are not admitted either)
func isNodeSchedulable(node *corev1.Node) bool {
	if node.Spec.Unschedulable {
		return false
	}

	for _, taint := range node.Spec.Taints {
		if taint.Effect == corev1.TaintEffectNoSchedule ||
			taint.Effect == corev1.TaintEffectNoExecute {
			return false
		}
	}

	return true
}
