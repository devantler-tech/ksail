package reconcilediag

import (
	"context"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

const argoCDNamespace = "argocd"

// argoCDGVRApplications returns the GVR for ArgoCD Applications.
func argoCDGVRApplications() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group: "argoproj.io", Version: "v1alpha1", Resource: "applications",
	}
}

// ArgoCDCollector gathers diagnostics for ArgoCD reconciliation failures.
type ArgoCDCollector struct {
	Dynamic   dynamic.Interface
	Clientset kubernetes.Interface
}

// Collect gathers a diagnostic report for ArgoCD failures.
// All sub-collectors are best-effort: individual failures are silently skipped.
func (c *ArgoCDCollector) Collect(ctx context.Context) *Report {
	report := &Report{
		EventNamespace: argoCDNamespace,
	}

	report.Sections = append(report.Sections, c.collectFailingApplications(ctx))
	report.FailingPods = c.collectFailingPods(ctx)
	report.Events = c.collectWarningEvents(ctx)

	return report
}

// collectFailingApplications lists ArgoCD Applications and returns those
// that are not synced+healthy.
// safeCollectCRs handles panic recovery (e.g., when CRDs are not installed) so
// diagnostic collection never crashes the CLI.
func (c *ArgoCDCollector) collectFailingApplications(ctx context.Context) ResourceSection {
	section := ResourceSection{Heading: "Failing Applications"}

	apps := safeCollectCRs(ctx, c.Dynamic, argoCDGVRApplications(), argoCDNamespace)

	for i := range apps {
		app := &apps[i]

		if isArgoCDAppHealthy(app) {
			continue
		}

		reason, message := describeArgoCDAppFailure(app)
		if reason == "" && message == "" {
			continue
		}

		section.Resources = append(section.Resources, FailingResource{
			Name:    app.GetName(),
			Reason:  reason,
			Message: message,
		})
	}

	return section
}

// isArgoCDAppHealthy checks if an Application is Synced+Healthy.
func isArgoCDAppHealthy(app *unstructured.Unstructured) bool {
	syncStatus, _, _ := unstructured.NestedString(app.Object, "status", "sync", "status")
	healthStatus, _, _ := unstructured.NestedString(app.Object, "status", "health", "status")

	return syncStatus == "Synced" && healthStatus == "Healthy"
}

// describeArgoCDAppFailure extracts a reason and message from a failing Application.
// It checks operation state, conditions, and sync/health status.
func describeArgoCDAppFailure(app *unstructured.Unstructured) (string, string) {
	// Check operation state for errors.
	if phase, msg := extractOperationFailure(app); phase != "" {
		return phase, msg
	}

	// Check conditions for errors.
	if reason, msg := extractConditionError(app); reason != "" {
		return reason, msg
	}

	// Fall back to sync + health status description.
	syncStatus, _, _ := unstructured.NestedString(app.Object, "status", "sync", "status")
	healthStatus, _, _ := unstructured.NestedString(app.Object, "status", "health", "status")
	healthMessage, _, _ := unstructured.NestedString(app.Object, "status", "health", "message")

	reason := syncStatus + "/" + healthStatus
	if healthMessage != "" {
		return reason, healthMessage
	}

	return reason, ""
}

// extractOperationFailure checks status.operationState for Error/Failed phases.
func extractOperationFailure(app *unstructured.Unstructured) (string, string) {
	opState, found, _ := unstructured.NestedMap(app.Object, "status", "operationState")
	if !found {
		return "", ""
	}

	phase, _, _ := unstructured.NestedString(opState, "phase")
	if phase != "Error" && phase != "Failed" {
		return "", ""
	}

	message, _, _ := unstructured.NestedString(opState, "message")

	return "OperationState/" + phase, message
}

// extractConditionError checks status.conditions for error-type conditions.
func extractConditionError(app *unstructured.Unstructured) (string, string) {
	conditions, found, _ := unstructured.NestedSlice(app.Object, "status", "conditions")
	if !found {
		return "", ""
	}

	errorTypes := map[string]bool{
		"ComparisonError": true,
		"SyncError":       true,
		"InvalidSpecError": true,
	}

	for _, cond := range conditions {
		condMap, ok := cond.(map[string]any)
		if !ok {
			continue
		}

		condType, _, _ := unstructured.NestedString(condMap, "type")
		if !errorTypes[condType] {
			continue
		}

		message, _, _ := unstructured.NestedString(condMap, "message")

		return condType, message
	}

	return "", ""
}

// collectFailingPods returns a pre-formatted string of failing pods in the ArgoCD namespace.
func (c *ArgoCDCollector) collectFailingPods(ctx context.Context) string {
	return k8s.DiagnosePodFailures(ctx, c.Clientset, []string{argoCDNamespace})
}

// collectWarningEvents returns recent warning events from the ArgoCD namespace.
func (c *ArgoCDCollector) collectWarningEvents(ctx context.Context) []WarningEvent {
	return collectNamespaceWarningEvents(ctx, c.Clientset, argoCDNamespace)
}
