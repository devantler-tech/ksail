package k8s

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// DiagnoseCluster produces a combined human-readable diagnostic report for
// a running Kubernetes cluster. It enumerates every namespace, surfaces any
// failing pods via DiagnosePodFailures, and reports any nodes that are not
// Ready. When everything appears healthy, the returned string is empty.
//
// The diagnostic is intentionally distribution-agnostic — it only relies on
// the Kubernetes API and therefore works for Vanilla, K3s, Talos, and
// VCluster alike. It is consumed by the `ksail cluster diagnose` command
// and surfaced to the Copilot chat/MCP tooling via the auto-generated
// `cluster_read` tool so AI assistants can reason over the output.
func DiagnoseCluster(ctx context.Context, clientset kubernetes.Interface) (string, error) {
	var builder strings.Builder

	nodeReport, err := diagnoseNodes(ctx, clientset)
	if err != nil {
		return "", err
	}

	if nodeReport != "" {
		builder.WriteString(nodeReport)
	}

	namespaces, err := listNamespaceNames(ctx, clientset)
	if err != nil {
		return "", err
	}

	podReport := DiagnosePodFailures(ctx, clientset, namespaces)
	if podReport != "" {
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}

		builder.WriteString(podReport)
	}

	return strings.Trim(builder.String(), "\n"), nil
}

// listNamespaceNames returns the names of every namespace in the cluster.
func listNamespaceNames(
	ctx context.Context,
	clientset kubernetes.Interface,
) ([]string, error) {
	nsList, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list namespaces: %w", err)
	}

	names := make([]string, 0, len(nsList.Items))
	for i := range nsList.Items {
		names = append(names, nsList.Items[i].Name)
	}

	return names, nil
}

// diagnoseNodes returns a human-readable summary of any nodes that are not
// Ready. Returns an empty string when all nodes are Ready.
func diagnoseNodes(ctx context.Context, clientset kubernetes.Interface) (string, error) {
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("list nodes: %w", err)
	}

	var builder strings.Builder

	for i := range nodes.Items {
		node := &nodes.Items[i]
		if reason := describeNotReadyNode(node); reason != "" {
			if builder.Len() == 0 {
				builder.WriteString("Not-Ready nodes:")
			}

			builder.WriteString("\n  ")
			builder.WriteString(reason)
		}
	}

	return builder.String(), nil
}

// describeNotReadyNode returns a one-line description when a node's Ready
// condition is not True, or an empty string when the node is Ready.
func describeNotReadyNode(node *corev1.Node) string {
	for _, cond := range node.Status.Conditions {
		if cond.Type != corev1.NodeReady {
			continue
		}

		if cond.Status == corev1.ConditionTrue {
			return ""
		}

		message := cond.Message
		if message == "" {
			message = cond.Reason
		}

		return fmt.Sprintf("%s: Ready=%s (%s)", node.Name, cond.Status, message)
	}

	return node.Name + ": Ready condition missing"
}

// DiagnosePodFailures checks pods in the given namespaces and returns a
// human-readable summary of any pods that are not running successfully.
// If all pods are healthy or no pods exist, it returns an empty string.
func DiagnosePodFailures(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespaces []string,
) string {
	var builder strings.Builder

	for _, namespace := range namespaces {
		pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			fmt.Fprintf(&builder, "\n  (failed to list pods in %s: %v)", namespace, err)

			continue
		}

		failures := collectPodFailures(pods.Items)
		if len(failures) == 0 {
			continue
		}

		fmt.Fprintf(&builder, "\nFailing pods in %s namespace:", namespace)

		for _, f := range failures {
			builder.WriteString("\n  ")
			builder.WriteString(f)
		}
	}

	return builder.String()
}

// collectPodFailures returns a line per unhealthy pod describing the problem.
func collectPodFailures(pods []corev1.Pod) []string {
	failures := make([]string, 0, len(pods))

	for i := range pods {
		pod := &pods[i]
		if isPodHealthy(pod) {
			continue
		}

		failures = append(failures, describePodFailure(pod))
	}

	return failures
}

// isPodHealthy returns true when a pod is Running with all containers ready,
// or Succeeded (completed Job pod).
func isPodHealthy(pod *corev1.Pod) bool {
	switch pod.Status.Phase {
	case corev1.PodRunning:
		for _, container := range pod.Status.ContainerStatuses {
			if !container.Ready {
				return false
			}
		}

		return true
	case corev1.PodSucceeded:
		return true
	case corev1.PodPending, corev1.PodFailed, corev1.PodUnknown:
		return false
	}

	return false
}

// describePodFailure returns a single-line description of why a pod is unhealthy.
func describePodFailure(pod *corev1.Pod) string {
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if desc := describeContainerWaiting(pod.Name, containerStatus); desc != "" {
			return desc
		}

		if desc := describeContainerTerminated(pod.Name, containerStatus); desc != "" {
			return desc
		}
	}

	for _, containerStatus := range pod.Status.InitContainerStatuses {
		if containerStatus.State.Waiting != nil && containerStatus.State.Waiting.Reason != "" {
			return fmt.Sprintf(
				"%s: init container %s: %s for %s",
				pod.Name,
				containerStatus.Name,
				containerStatus.State.Waiting.Reason,
				containerStatus.Image,
			)
		}
	}

	if pod.Status.Reason != "" {
		return fmt.Sprintf("%s: %s (%s)", pod.Name, pod.Status.Phase, pod.Status.Reason)
	}

	return fmt.Sprintf("%s: %s", pod.Name, pod.Status.Phase)
}

// describeContainerWaiting returns a description when a container is stuck waiting with a reason.
func describeContainerWaiting(podName string, containerStatus corev1.ContainerStatus) string {
	if containerStatus.State.Waiting == nil || containerStatus.State.Waiting.Reason == "" {
		return ""
	}

	desc := fmt.Sprintf(
		"%s: %s for %s",
		podName,
		containerStatus.State.Waiting.Reason,
		containerStatus.Image,
	)
	if containerStatus.RestartCount == 1 {
		desc += " (1 restart)"
	} else if containerStatus.RestartCount > 1 {
		desc += fmt.Sprintf(" (%d restarts)", containerStatus.RestartCount)
	}

	return desc
}

// describeContainerTerminated returns a description when a container exited with a non-zero code.
func describeContainerTerminated(podName string, containerStatus corev1.ContainerStatus) string {
	if containerStatus.State.Terminated == nil || containerStatus.State.Terminated.ExitCode == 0 {
		return ""
	}

	desc := fmt.Sprintf(
		"%s: terminated with exit code %d (%s)",
		podName, containerStatus.State.Terminated.ExitCode, containerStatus.State.Terminated.Reason,
	)
	if containerStatus.RestartCount == 1 {
		desc += " (1 restart)"
	} else if containerStatus.RestartCount > 1 {
		desc += fmt.Sprintf(" (%d restarts)", containerStatus.RestartCount)
	}

	return desc
}
