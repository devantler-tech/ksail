package reconcilediag

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/notify"
)

// FailingResource describes a GitOps custom resource that is not ready.
type FailingResource struct {
	// Name is the resource name (e.g., "flux-system").
	Name string
	// Namespace is the resource namespace (empty for cluster-scoped or default-namespace resources).
	Namespace string
	// Reason is the condition reason (e.g., "ReconciliationFailed", "HealthCheckFailed").
	Reason string
	// Message is the condition message with details about the failure.
	Message string
}

// String returns a single-line description of the failing resource.
func (f FailingResource) String() string {
	prefix := f.Name
	if f.Namespace != "" {
		prefix = f.Namespace + "/" + f.Name
	}

	if f.Reason != "" && f.Message != "" {
		return fmt.Sprintf("%s: %s — %s", prefix, f.Reason, f.Message)
	}

	if f.Reason != "" {
		return fmt.Sprintf("%s: %s", prefix, f.Reason)
	}

	if f.Message != "" {
		return fmt.Sprintf("%s: %s", prefix, f.Message)
	}

	return prefix + ": not ready"
}

// WarningEvent describes a recent warning event from Kubernetes.
type WarningEvent struct {
	// Age is how long ago the event occurred.
	Age time.Duration
	// Kind is the involved object kind (e.g., "Pod", "HelmRelease").
	Kind string
	// Name is the involved object name.
	Name string
	// Namespace is the involved object namespace.
	Namespace string
	// Message is the event message.
	Message string
}

// String returns a single-line description of the event.
func (e WarningEvent) String() string {
	return fmt.Sprintf("%s ago: %s/%s — %s", formatDuration(e.Age), e.Kind, e.Name, e.Message)
}

// ResourceSection groups failing resources under a heading.
type ResourceSection struct {
	// Heading describes the resource type (e.g., "Failing Kustomizations").
	Heading string
	// Resources are the failing resources in this section.
	Resources []FailingResource
}

// Report holds the collected diagnostic data for a failed reconciliation.
type Report struct {
	// Sections contains groups of failing GitOps resources.
	Sections []ResourceSection
	// FailingPods is a pre-formatted string from DiagnosePodFailures (may be empty).
	FailingPods string
	// Events are recent warning events from the GitOps namespace.
	Events []WarningEvent
	// EventNamespace is the namespace events were collected from.
	EventNamespace string
}

// IsEmpty returns true when the report contains no diagnostic data.
func (r *Report) IsEmpty() bool {
	for _, s := range r.Sections {
		if len(s.Resources) > 0 {
			return false
		}
	}

	return r.FailingPods == "" && len(r.Events) == 0
}

// maxDiagnosticEvents limits the number of warning events shown.
const maxDiagnosticEvents = 20

// Write writes the diagnostic report to the given writer using the
// notify package for consistent styling.
func (r *Report) Write(w io.Writer) {
	if r.IsEmpty() {
		return
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Emoji:   "🩺",
		Content: "Reconciliation Diagnostics",
		Writer:  w,
	})

	for _, section := range r.Sections {
		if len(section.Resources) == 0 {
			continue
		}

		notify.Errorf(w, "%s:", section.Heading)

		for _, res := range section.Resources {
			fmt.Fprintf(w, "    %s\n", res.String())
		}
	}

	if r.FailingPods != "" {
		notify.Warningf(w, "failing pods (%s):", r.EventNamespace)

		for _, line := range strings.Split(strings.TrimSpace(r.FailingPods), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				fmt.Fprintf(w, "    %s\n", line)
			}
		}
	}

	if len(r.Events) > 0 {
		label := fmt.Sprintf("warning events (%s, last 5 min)", r.EventNamespace)
		notify.Warningf(w, "%s:", label)

		limit := len(r.Events)
		if limit > maxDiagnosticEvents {
			limit = maxDiagnosticEvents
		}

		for _, evt := range r.Events[:limit] {
			fmt.Fprintf(w, "    %s\n", evt.String())
		}

		if len(r.Events) > maxDiagnosticEvents {
			fmt.Fprintf(w, "    ... and %d more events\n", len(r.Events)-maxDiagnosticEvents)
		}
	}
}

// formatDuration returns a human-friendly short duration string.
func formatDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
}
