package k8s

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// DiagnoseSeverity represents the severity level of a diagnostic finding.
type DiagnoseSeverity string

const (
	// DiagnoseSeverityCritical indicates a resource is failing and requires immediate attention.
	DiagnoseSeverityCritical DiagnoseSeverity = "critical"
	// DiagnoseSeverityWarning indicates a resource is degraded but not yet failing.
	DiagnoseSeverityWarning DiagnoseSeverity = "warning"
)

const (
	// diagnoseMaxHealthScore is the starting score for a fully healthy cluster.
	diagnoseMaxHealthScore = 100
	// diagnoseCriticalPenalty is the score deduction for each critical finding.
	diagnoseCriticalPenalty = 25
	// diagnoseWarningPenalty is the score deduction for each warning finding.
	diagnoseWarningPenalty = 10
)

// DiagnoseFinding describes a single unhealthy resource detected during diagnosis.
type DiagnoseFinding struct {
	// Severity is the impact level: critical or warning.
	Severity DiagnoseSeverity `json:"severity"`
	// Resource is a short identifier, e.g. "node/node-1" or "pod/boom (default)".
	Resource string `json:"resource"`
	// Reason is a one-line description of the failure.
	Reason string `json:"reason"`
	// Remediation is an actionable hint for resolving the failure, or empty
	// when no known remediation exists for the detected reason.
	Remediation string `json:"remediation,omitempty"`
}

// remediationNotReadyNode is the remediation hint for nodes whose Ready
// condition is not True.
const remediationNotReadyNode = "Check kubelet status and node conditions for disk, memory, or PID pressure."

// remediationPVCPending is the remediation hint for PersistentVolumeClaims
// stuck in Pending phase.
const remediationPVCPending = "Verify a matching StorageClass exists and the provisioner is running. " +
	"Check PVC events with 'ksail workload describe pvc/<name>'."

// lookupRemediation returns a remediation hint for the given reason string
// by checking for known substring matches against common Kubernetes failure
// patterns. Returns an empty string when no hint is available.
//

func lookupRemediation(reason string) string {
	type hint struct {
		pattern string
		message string
	}

	hints := [...]hint{
		{
			"CrashLoopBackOff",
			"Check container logs with 'ksail workload logs <pod>' for the crash reason. " +
				"Common causes: misconfigured entrypoint, missing config/secrets, or application error.",
		},
		{
			"ImagePullBackOff",
			"Verify the image name and tag exist in the registry. " +
				"Check registry credentials and network connectivity.",
		},
		{
			"ErrImagePull",
			"Verify the image name and tag exist in the registry. " +
				"Check registry credentials and network connectivity.",
		},
		{
			"CreateContainerConfigError",
			"Check that referenced ConfigMaps and Secrets exist and are correctly mounted.",
		},
		{
			"OOMKilled",
			"Container exceeded its memory limit. " +
				"Increase resources.limits.memory or reduce application memory usage.",
		},
		{
			"Evicted",
			"Pod was evicted due to node resource pressure. Check node disk and memory usage.",
		},
		{
			"Ready condition missing",
			"Node is not reporting a Ready condition. Check kubelet status and node logs.",
		},
	}

	for _, h := range hints {
		if strings.Contains(reason, h.pattern) {
			return h.message
		}
	}

	return ""
}

// DiagnoseReport is the structured result of DiagnoseClusterReport. It is
// the JSON-serialisable form of the cluster health snapshot produced by
// DiagnoseCluster. The HealthScore field (0–100) gives AI assistants and
// automation a single numeric signal; Findings carry the details.
type DiagnoseReport struct {
	// ClusterName is the name of the inspected cluster.
	ClusterName string `json:"clusterName"`
	// HealthScore is an integer from 0 (completely broken) to 100 (fully healthy).
	// Each critical finding deducts 25 points; each warning deducts 10 points.
	HealthScore int `json:"healthScore"`
	// Findings lists every unhealthy resource discovered.
	Findings []DiagnoseFinding `json:"findings"`
}

// DiagnoseClusterReport is the structured equivalent of DiagnoseCluster. It
// returns a DiagnoseReport suitable for JSON serialisation and AI consumption
// via the cluster_read MCP tool. The plain-text representation produced by
// DiagnoseCluster remains the default; this function is used when the caller
// requests --format json.
func DiagnoseClusterReport(
	ctx context.Context,
	clientset kubernetes.Interface,
	clusterName string,
) (DiagnoseReport, error) {
	report := DiagnoseReport{
		ClusterName: clusterName,
		HealthScore: diagnoseMaxHealthScore,
		Findings:    []DiagnoseFinding{},
	}

	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return report, fmt.Errorf("list nodes: %w", err)
	}

	for i := range nodes.Items {
		node := &nodes.Items[i]
		if reason := describeNotReadyNode(node); reason != "" {
			report.Findings = append(report.Findings, DiagnoseFinding{
				Severity:    DiagnoseSeverityCritical,
				Resource:    "node/" + node.Name,
				Reason:      reason,
				Remediation: remediationNotReadyNode,
			})
		}
	}

	namespaces, err := listNamespaceNames(ctx, clientset)
	if err != nil {
		return report, err
	}

	for _, namespace := range namespaces {
		appendNamespacePodFindings(ctx, clientset, namespace, &report.Findings)
		appendNamespacePVCFindings(ctx, clientset, namespace, &report.Findings)
	}

	report.HealthScore = diagnoseComputeScore(report.Findings)

	return report, nil
}

// appendNamespacePodFindings lists all pods in namespace and appends a finding
// for each unhealthy pod (or a warning finding when the pod list itself fails).
func appendNamespacePodFindings(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespace string,
	findings *[]DiagnoseFinding,
) {
	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		*findings = append(*findings, DiagnoseFinding{
			Severity:    DiagnoseSeverityWarning,
			Resource:    "namespace/" + namespace,
			Reason:      fmt.Sprintf("failed to list pods: %v", err),
			Remediation: "Check cluster connectivity and RBAC permissions.",
		})

		return
	}

	for j := range pods.Items {
		pod := &pods.Items[j]
		if isPodHealthy(pod) {
			continue
		}

		reason := describePodFailure(pod)
		*findings = append(*findings, DiagnoseFinding{
			Severity:    DiagnoseSeverityCritical,
			Resource:    fmt.Sprintf("pod/%s (%s)", pod.Name, namespace),
			Reason:      reason,
			Remediation: lookupRemediation(reason),
		})
	}
}

// appendNamespacePVCFindings lists all PersistentVolumeClaims in namespace
// and appends a warning finding for each PVC stuck in Pending phase.
func appendNamespacePVCFindings(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespace string,
	findings *[]DiagnoseFinding,
) {
	pvcs, err := clientset.CoreV1().
		PersistentVolumeClaims(namespace).
		List(ctx, metav1.ListOptions{})
	if err != nil {
		*findings = append(*findings, DiagnoseFinding{
			Severity:    DiagnoseSeverityWarning,
			Resource:    "namespace/" + namespace,
			Reason:      fmt.Sprintf("failed to list PVCs: %v", err),
			Remediation: "Check cluster connectivity and RBAC permissions.",
		})

		return
	}

	for i := range pvcs.Items {
		pvc := &pvcs.Items[i]
		if pvc.Status.Phase != corev1.ClaimPending {
			continue
		}

		*findings = append(*findings, DiagnoseFinding{
			Severity:    DiagnoseSeverityWarning,
			Resource:    fmt.Sprintf("pvc/%s (%s)", pvc.Name, namespace),
			Reason:      "PVC is stuck in Pending phase",
			Remediation: remediationPVCPending,
		})
	}
}

// diagnoseComputeScore returns a health score in [0, diagnoseMaxHealthScore]
// by deducting penalty points for each finding.
func diagnoseComputeScore(findings []DiagnoseFinding) int {
	score := diagnoseMaxHealthScore

	for _, f := range findings {
		switch f.Severity {
		case DiagnoseSeverityCritical:
			score -= diagnoseCriticalPenalty
		case DiagnoseSeverityWarning:
			score -= diagnoseWarningPenalty
		}
	}

	if score < 0 {
		score = 0
	}

	return score
}

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

	pvcReport := DiagnosePVCPending(ctx, clientset, namespaces)
	if pvcReport != "" {
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}

		builder.WriteString(pvcReport)
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

// DiagnosePVCPending checks PersistentVolumeClaims in the given namespaces
// and returns a human-readable summary of any PVCs stuck in Pending phase.
// If no PVCs are pending, it returns an empty string.
func DiagnosePVCPending(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespaces []string,
) string {
	var builder strings.Builder

	for _, namespace := range namespaces {
		pvcs, err := clientset.CoreV1().
			PersistentVolumeClaims(namespace).
			List(ctx, metav1.ListOptions{})
		if err != nil {
			fmt.Fprintf(&builder, "\n  (failed to list PVCs in %s: %v)", namespace, err)

			continue
		}

		var pending []string

		for i := range pvcs.Items {
			if pvcs.Items[i].Status.Phase == corev1.ClaimPending {
				pending = append(pending, pvcs.Items[i].Name)
			}
		}

		if len(pending) == 0 {
			continue
		}

		fmt.Fprintf(&builder, "\nPending PVCs in %s namespace:", namespace)

		for _, name := range pending {
			builder.WriteString("\n  ")
			builder.WriteString(name)
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
