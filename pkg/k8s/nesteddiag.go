package k8s

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// nestedDiagLogTail bounds the per-container log tail captured by DumpNamespaceDiagnostics.
	nestedDiagLogTail int64 = 60
	// nestedDiagTimeout bounds the API queries DumpNamespaceDiagnostics performs.
	nestedDiagTimeout = 30 * time.Second
)

// DumpNamespaceDiagnostics returns a detailed, human-readable snapshot of every pod in the
// given namespaces: each pod's phase, per-container readiness/restart-count/state/image, any
// non-True pod conditions (e.g. PodScheduled=False), recent events, and a tail of each
// container's logs. It is intended for root-causing why a nested control-plane pod (such as a
// k3k server or a vCluster) fails to become Ready on a given host.
//
// It detaches from the caller's cancellation and deadline (via context.WithoutCancel) and runs
// under its own short timeout, so it still works when called from a failure path whose original
// context has already expired (e.g. after a readiness poll times out). All sub-queries are
// best-effort: partial failures are reported inline rather than aborting the dump.
func DumpNamespaceDiagnostics(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespaces ...string,
) string {
	diagCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), nestedDiagTimeout)
	defer cancel()

	var builder strings.Builder

	for _, namespace := range namespaces {
		dumpNamespace(diagCtx, &builder, clientset, namespace)
	}

	return builder.String()
}

// dumpNamespace appends the pod summaries, events, and logs for a single namespace.
func dumpNamespace(
	ctx context.Context,
	builder *strings.Builder,
	clientset kubernetes.Interface,
	namespace string,
) {
	fmt.Fprintf(builder, "\n=== diagnostics: namespace %q pods ===", namespace)

	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(builder, "\n  (list pods failed: %v)", err)

		return
	}

	if len(pods.Items) == 0 {
		builder.WriteString("\n  (no pods)")
	}

	for idx := range pods.Items {
		writePodSummary(builder, &pods.Items[idx])
	}

	fmt.Fprintf(builder, "\n=== diagnostics: namespace %q events ===", namespace)
	writeNamespaceEvents(ctx, builder, clientset, namespace)

	fmt.Fprintf(builder, "\n=== diagnostics: namespace %q pod logs (tail %d) ===",
		namespace, nestedDiagLogTail)

	for idx := range pods.Items {
		writePodLogs(ctx, builder, clientset, &pods.Items[idx])
	}
}

// writePodSummary appends a pod's phase plus per-container state and any non-True conditions.
func writePodSummary(builder *strings.Builder, pod *corev1.Pod) {
	ready := 0

	for idx := range pod.Status.ContainerStatuses {
		if pod.Status.ContainerStatuses[idx].Ready {
			ready++
		}
	}

	// Pending/initializing pods often have no ContainerStatuses yet, so use the spec
	// container count as the expected total (falling back to the status count) to avoid
	// a misleading "ready=0/0" for a pod that is still pulling images or scheduling.
	total := len(pod.Spec.Containers)
	if total == 0 {
		total = len(pod.Status.ContainerStatuses)
	}

	fmt.Fprintf(builder, "\n  pod %s: phase=%s ready=%d/%d node=%q",
		pod.Name, pod.Status.Phase, ready, total, pod.Spec.NodeName)

	for idx := range pod.Status.ContainerStatuses {
		status := pod.Status.ContainerStatuses[idx]
		fmt.Fprintf(
			builder,
			"\n    container %s: ready=%t restarts=%d state=%s image=%q",
			status.Name,
			status.Ready,
			status.RestartCount,
			containerStateString(status),
			status.Image,
		)
	}

	for idx := range pod.Status.Conditions {
		cond := pod.Status.Conditions[idx]
		if cond.Status != corev1.ConditionTrue {
			fmt.Fprintf(builder, "\n    condition %s=%s reason=%q msg=%q",
				cond.Type, cond.Status, cond.Reason, cond.Message)
		}
	}
}

// containerStateString renders a container's current state (waiting/terminated/running).
func containerStateString(status corev1.ContainerStatus) string {
	switch {
	case status.State.Waiting != nil:
		return fmt.Sprintf("Waiting(reason=%q msg=%q)",
			status.State.Waiting.Reason, status.State.Waiting.Message)
	case status.State.Terminated != nil:
		return fmt.Sprintf("Terminated(reason=%q exit=%d)",
			status.State.Terminated.Reason, status.State.Terminated.ExitCode)
	case status.State.Running != nil:
		return "Running"
	default:
		return "Unknown"
	}
}

// writeNamespaceEvents appends the namespace's events, oldest first.
func writeNamespaceEvents(
	ctx context.Context,
	builder *strings.Builder,
	clientset kubernetes.Interface,
	namespace string,
) {
	events, err := clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(builder, "\n  (list events failed: %v)", err)

		return
	}

	if len(events.Items) == 0 {
		builder.WriteString("\n  (no events)")

		return
	}

	sorted := make([]*corev1.Event, len(events.Items))
	for idx := range events.Items {
		sorted[idx] = &events.Items[idx]
	}

	slices.SortFunc(sorted, func(left, right *corev1.Event) int {
		return cmp.Compare(nestedEventTime(left).UnixNano(), nestedEventTime(right).UnixNano())
	})

	for _, evt := range sorted {
		fmt.Fprintf(builder, "\n  %s %s %s/%s: %s",
			evt.Type, evt.Reason, evt.InvolvedObject.Kind, evt.InvolvedObject.Name, evt.Message)
	}
}

// nestedEventTime returns the most relevant timestamp for an event.
func nestedEventTime(evt *corev1.Event) time.Time {
	if !evt.LastTimestamp.IsZero() {
		return evt.LastTimestamp.Time
	}

	if !evt.EventTime.IsZero() {
		return evt.EventTime.Time
	}

	return evt.CreationTimestamp.Time
}

// writePodLogs appends a tail of each container's logs for the given pod.
func writePodLogs(
	ctx context.Context,
	builder *strings.Builder,
	clientset kubernetes.Interface,
	pod *corev1.Pod,
) {
	for idx := range pod.Spec.Containers {
		container := pod.Spec.Containers[idx].Name
		fmt.Fprintf(
			builder,
			"\n  --- %s/%s ---\n%s",
			pod.Name,
			container,
			podContainerLogs(ctx, clientset, pod.Namespace, pod.Name, container),
		)
	}
}

// podContainerLogs returns a bounded tail of a container's logs, or a parenthetical note
// when logs are unavailable (e.g. the container has not started yet).
func podContainerLogs(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespace, pod, container string,
) string {
	tail := nestedDiagLogTail

	stream, err := clientset.CoreV1().Pods(namespace).GetLogs(pod, &corev1.PodLogOptions{
		Container: container,
		TailLines: &tail,
	}).Stream(ctx)
	if err != nil {
		return fmt.Sprintf("(get logs failed: %v)", err)
	}

	defer func() { _ = stream.Close() }()

	data, err := io.ReadAll(stream)
	if err != nil {
		return fmt.Sprintf("(read logs failed: %v)", err)
	}

	if len(data) == 0 {
		return "(no logs)"
	}

	return string(data)
}
