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
// When Namespace is set, the object is formatted as Kind/Namespace/Name.
func (e WarningEvent) String() string {
	obj := e.Kind + "/" + e.Name
	if e.Namespace != "" {
		obj = e.Kind + "/" + e.Namespace + "/" + e.Name
	}

	return fmt.Sprintf("%s ago: %s — %s", formatDuration(e.Age), obj, e.Message)
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
	// EventLookback is the lookback window used when collecting events.
	// Used to derive the human-readable label in the diagnostics output.
	EventLookback time.Duration
}

// IsEmpty returns true when the report contains no diagnostic data.
func (r *Report) IsEmpty() bool {
	for _, s := range r.Sections {
		if len(s.Resources) > 0 {
			return false
		}
	}

	return strings.TrimSpace(r.FailingPods) == "" && len(r.Events) == 0
}

const (
	// defaultEventLookback is the default lookback window for warning events.
	// Both FluxCollector and ArgoCDCollector use this value.
	defaultEventLookback = 5 * time.Minute
	// maxDiagnosticEvents limits the number of warning events shown.
	maxDiagnosticEvents = 20
)

// Write writes the diagnostic report to the given writer using the
// notify package for consistent styling.
func (r *Report) Write(writer io.Writer) {
	if r.IsEmpty() {
		return
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Emoji:   "🩺",
		Content: "Reconciliation Diagnostics",
		Writer:  writer,
	})

	r.writeSections(writer)
	r.writeFailingPods(writer)
	r.writeEvents(writer)
}

// writeSections writes the failing resource sections.
func (r *Report) writeSections(writer io.Writer) {
	for _, section := range r.Sections {
		if len(section.Resources) == 0 {
			continue
		}

		notify.Errorf(writer, "%s:", section.Heading)

		for _, res := range section.Resources {
			_, _ = fmt.Fprintf(writer, "    %s\n", res.String())
		}
	}
}

// writeFailingPods writes the failing pods section.
func (r *Report) writeFailingPods(writer io.Writer) {
	if strings.TrimSpace(r.FailingPods) == "" {
		return
	}

	notify.Warningf(writer, "failing pods (%s):", r.EventNamespace)

	for _, line := range strings.Split(strings.TrimSpace(r.FailingPods), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			_, _ = fmt.Fprintf(writer, "    %s\n", line)
		}
	}
}

// writeEvents writes the warning events section.
func (r *Report) writeEvents(writer io.Writer) {
	if len(r.Events) == 0 {
		return
	}

	lookback := r.EventLookback
	if lookback <= 0 {
		lookback = defaultEventLookback
	}

	label := fmt.Sprintf("warning events (%s, last %s)", r.EventNamespace, formatDuration(lookback))
	notify.Warningf(writer, "%s:", label)

	limit := min(len(r.Events), maxDiagnosticEvents)

	for _, evt := range r.Events[:limit] {
		_, _ = fmt.Fprintf(writer, "    %s\n", evt.String())
	}

	if len(r.Events) > maxDiagnosticEvents {
		_, _ = fmt.Fprintf(
			writer,
			"    ... and %d more events\n",
			len(r.Events)-maxDiagnosticEvents,
		)
	}
}

// minutesPerHour is used for duration formatting calculations.
const minutesPerHour = 60

// formatDuration returns a human-friendly short duration string.
func formatDuration(duration time.Duration) string {
	switch {
	case duration < time.Minute:
		return fmt.Sprintf("%ds", int(duration.Seconds()))
	case duration < time.Hour:
		return fmt.Sprintf("%dm", int(duration.Minutes()))
	default:
		return fmt.Sprintf("%dh%dm", int(duration.Hours()), int(duration.Minutes())%minutesPerHour)
	}
}
