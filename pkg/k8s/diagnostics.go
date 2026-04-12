package k8s

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

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
				pod.Name, containerStatus.Name, containerStatus.State.Waiting.Reason, containerStatus.Image,
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

	desc := fmt.Sprintf("%s: %s for %s", podName, containerStatus.State.Waiting.Reason, containerStatus.Image)
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
