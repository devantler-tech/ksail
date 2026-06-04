package reconcilediag

import (
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/notify"
)

// resourceState classifies a failing resource for display. The state controls
// the symbol shown, the sort order (roots before cascades), and whether the
// resource counts as a root cause in the report summary.
type resourceState int

const (
	// stateFailed is a resource that failed on its own (e.g., HealthCheckFailed).
	stateFailed resourceState = iota
	// stateProgressing is a resource still actively reconciling when the snapshot
	// was taken (e.g., a Helm upgrade in progress) — a likely root cause.
	stateProgressing
	// stateBlocked is a resource waiting on an unready dependency. These are
	// cascade fallout, not root causes, so they sort last and are de-emphasized.
	stateBlocked
)

const (
	// symbolFailed marks a resource that failed on its own.
	symbolFailed = "✗"
	// symbolProgressing marks a resource still reconciling.
	symbolProgressing = "►"
	// symbolBlocked marks a resource blocked on a dependency.
	symbolBlocked = "·"
)

const (
	// maxDetailLen caps the length of a cleaned condition/event message so a
	// single verbose Flux message cannot blow up a row.
	maxDetailLen = 100
	// blockedReason is the Flux condition reason for a resource waiting on a
	// dependency. Such resources are cascade fallout, not root causes.
	blockedReason = "DependencyNotReady"
	// rowIndent is the leading indent for each resource row.
	rowIndent = "    "
	// sectionIndent is the leading indent for a section heading.
	sectionIndent = "  "
)

// dependencyMessageRe extracts the dependency name from a "DependencyNotReady"
// message such as: dependency 'flux-system/infrastructure' is not ready.
var dependencyMessageRe = regexp.MustCompile(`dependency '([^']+)'`)

// fractionalSecondsRe strips sub-second precision from Go durations so
// "25m0.04814184s" renders as "25m0s" instead of a wall of digits.
var fractionalSecondsRe = regexp.MustCompile(`(\d+)\.\d+s`)

// zeroSecondsRe trims a trailing "0s" left after a minutes component so
// "25m0s" renders as "25m".
var zeroSecondsRe = regexp.MustCompile(`(\d+m)0s\b`)

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
	return f.displayName() + ": " + f.detail()
}

// displayName returns "namespace/name" when a namespace is set, otherwise "name".
func (f FailingResource) displayName() string {
	if f.Namespace != "" {
		return f.Namespace + "/" + f.Name
	}

	return f.Name
}

// state classifies the resource from its condition reason.
func (f FailingResource) state() resourceState {
	return classifyReason(f.Reason)
}

// detail returns the right-hand description shown after the resource name.
// Blocked resources collapse to "blocked by <dependency>"; everything else
// shows "<Reason> · <cleaned message>".
func (f FailingResource) detail() string {
	if f.state() == stateBlocked {
		if dep := extractBlocker(f.Message); dep != "" {
			return "blocked by " + dep
		}

		return "blocked (dependency not ready)"
	}

	reason := f.Reason
	message := cleanMessage(f.Message)

	switch {
	case reason != "" && message != "":
		return reason + " · " + message
	case reason != "":
		return reason
	case message != "":
		return message
	default:
		return "not ready"
	}
}

// classifyReason maps a condition reason to a display state.
func classifyReason(reason string) resourceState {
	switch {
	case strings.Contains(reason, blockedReason):
		return stateBlocked
	case strings.Contains(reason, "Progress"):
		return stateProgressing
	default:
		return stateFailed
	}
}

// stateSymbol returns the leading symbol for a resource state.
func stateSymbol(state resourceState) string {
	switch state {
	case stateProgressing:
		return symbolProgressing
	case stateBlocked:
		return symbolBlocked
	case stateFailed:
		return symbolFailed
	default:
		return symbolFailed
	}
}

// extractBlocker pulls the dependency name from a DependencyNotReady message
// and strips the namespace prefix (e.g., "flux-system/infra" → "infra") so the
// row reads "blocked by infra". Returns "" when no dependency is named.
func extractBlocker(message string) string {
	match := dependencyMessageRe.FindStringSubmatch(message)
	if match == nil {
		return ""
	}

	dep := match[1]
	if idx := strings.LastIndex(dep, "/"); idx >= 0 {
		dep = dep[idx+1:]
	}

	return dep
}

// dependencyKey pulls the dependency reference from a DependencyNotReady message
// as a key that matches the dependency's displayName: the Flux default namespace
// ("flux-system/") prefix is stripped so default-namespace resources match their
// bare displayName, while any other namespace is preserved so same-named
// resources in different namespaces stay distinct. Returns "" when no dependency
// is named. Used for dependency-depth ordering; extractBlocker is used for the
// (intentionally bare) "blocked by" row text.
func dependencyKey(message string) string {
	match := dependencyMessageRe.FindStringSubmatch(message)
	if match == nil {
		return ""
	}

	return strings.TrimPrefix(match[1], fluxNamespace+"/")
}

// cleanMessage normalizes a condition or event message for compact display:
// it collapses whitespace, compresses bracketed resource lists to a count,
// strips sub-second duration precision, and truncates to maxDetailLen.
func cleanMessage(message string) string {
	cleaned := strings.Join(strings.Fields(message), " ")
	cleaned = compressBracketList(cleaned)
	cleaned = fractionalSecondsRe.ReplaceAllString(cleaned, "${1}s")
	cleaned = zeroSecondsRe.ReplaceAllString(cleaned, "$1")
	cleaned = strings.TrimSpace(cleaned)

	return truncate(cleaned, maxDetailLen)
}

// compressBracketList replaces a verbose bracketed list such as
// "[HelmRelease/a status: 'InProgress', HelmRelease/b status: 'InProgress']"
// with a short "N resources" count. Messages without a bracketed list are
// returned unchanged.
func compressBracketList(message string) string {
	open := strings.Index(message, "[")
	if open < 0 {
		return message
	}

	closeRel := strings.Index(message[open:], "]")
	if closeRel < 0 {
		return message
	}

	closeIdx := open + closeRel
	inner := strings.TrimSpace(message[open+1 : closeIdx])

	count := strings.Count(inner, "status:")
	if count == 0 && inner != "" {
		count = strings.Count(inner, ",") + 1
	}

	before := strings.TrimRight(message[:open], " :")
	after := message[closeIdx+1:]

	if count <= 0 {
		return strings.TrimSpace(before + after)
	}

	return fmt.Sprintf("%s %d resources%s", before, count, after)
}

// truncate shortens text to at most maxLen runes, appending an ellipsis when cut.
func truncate(text string, maxLen int) string {
	runes := []rune(text)
	if len(runes) <= maxLen {
		return text
	}

	return string(runes[:maxLen-1]) + "…"
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

// String returns a compact single-line description of the event. The message is
// cleaned (durations trimmed, resource lists compressed) and the object is shown
// as Kind/Name; the namespace is omitted since events are grouped per namespace.
func (e WarningEvent) String() string {
	return fmt.Sprintf(
		"%s ago  %s/%s  %s",
		formatDuration(e.Age),
		e.Kind,
		e.Name,
		cleanMessage(e.Message),
	)
}

// ResourceSection groups failing resources under a heading.
type ResourceSection struct {
	// Heading describes the resource type (e.g., "Kustomizations").
	Heading string
	// Resources are the failing resources in this section.
	Resources []FailingResource
}

const (
	// defaultEventLookback is the default lookback window for warning events.
	// Both FluxCollector and ArgoCDCollector use this value.
	defaultEventLookback = 5 * time.Minute
	// maxDiagnosticEvents limits the number of warning events shown.
	maxDiagnosticEvents = 20
)

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
	if r == nil {
		return true
	}

	for _, s := range r.Sections {
		if len(s.Resources) > 0 {
			return false
		}
	}

	return strings.TrimSpace(r.FailingPods) == "" && len(r.Events) == 0
}

// Write writes the diagnostic report to the given writer using the
// notify package for consistent styling.
func (r *Report) Write(writer io.Writer) {
	if r.IsEmpty() {
		return
	}

	notify.Errorf(writer, "🩺 Reconciliation failed")

	nameWidth := r.nameColumnWidth()

	r.writeSections(writer, nameWidth)
	r.writeFailingPods(writer)
	r.writeEvents(writer)
}

// Summary returns a concise one-line description of the failure suitable for
// use as the command's returned error. Root causes (resources that failed or
// are still reconciling) are named; cascade fallout is reduced to a count.
// Returns "" when the report holds no actionable resource data.
func (r *Report) Summary() string {
	if r.IsEmpty() {
		return ""
	}

	var roots []FailingResource

	blocked := 0

	for _, section := range r.Sections {
		for _, res := range section.Resources {
			if res.state() == stateBlocked {
				blocked++

				continue
			}

			roots = append(roots, res)
		}
	}

	return formatSummary(sortedResources(roots), blocked)
}

// nameColumnWidth returns the display width to pad resource names to, so the
// detail column aligns across every section in the report.
func (r *Report) nameColumnWidth() int {
	width := 0

	for _, section := range r.Sections {
		for _, res := range section.Resources {
			if n := len([]rune(res.displayName())); n > width {
				width = n
			}
		}
	}

	return width
}

// writeSections writes the failing resource sections, roots before cascades.
func (r *Report) writeSections(writer io.Writer, nameWidth int) {
	for _, section := range r.Sections {
		if len(section.Resources) == 0 {
			continue
		}

		_, _ = fmt.Fprintf(writer, "\n%s%s\n", sectionIndent, section.Heading)

		for _, res := range sortedResources(section.Resources) {
			_, _ = fmt.Fprintf(
				writer,
				"%s%s %-*s  %s\n",
				rowIndent, stateSymbol(res.state()), nameWidth, res.displayName(), res.detail(),
			)
		}
	}
}

// writeFailingPods writes the failing pods section.
func (r *Report) writeFailingPods(writer io.Writer) {
	if strings.TrimSpace(r.FailingPods) == "" {
		return
	}

	_, _ = fmt.Fprintln(writer)

	if r.EventNamespace != "" {
		notify.Warningf(writer, "failing pods (%s):", r.EventNamespace)
	} else {
		notify.Warningf(writer, "failing pods:")
	}

	for line := range strings.SplitSeq(strings.TrimSpace(r.FailingPods), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			_, _ = fmt.Fprintf(writer, "%s%s\n", rowIndent, line)
		}
	}
}

// writeEvents writes the warning events section.
func (r *Report) writeEvents(writer io.Writer) {
	if len(r.Events) == 0 {
		return
	}

	_, _ = fmt.Fprintln(writer)

	lookback := r.EventLookback
	if lookback <= 0 {
		lookback = defaultEventLookback
	}

	var label string
	if r.EventNamespace != "" {
		label = fmt.Sprintf(
			"recent warnings (%s, last %s)",
			r.EventNamespace,
			formatDuration(lookback),
		)
	} else {
		label = fmt.Sprintf("recent warnings (last %s)", formatDuration(lookback))
	}

	notify.Warningf(writer, "%s:", label)

	limit := min(len(r.Events), maxDiagnosticEvents)

	for _, evt := range r.Events[:limit] {
		_, _ = fmt.Fprintf(writer, "%s%s\n", rowIndent, evt.String())
	}

	if len(r.Events) > maxDiagnosticEvents {
		_, _ = fmt.Fprintf(
			writer,
			"%s... and %d more events\n",
			rowIndent,
			len(r.Events)-maxDiagnosticEvents,
		)
	}
}

// sortedResources returns the resources ordered by state (failed, then
// progressing, then blocked) so root causes appear above the cascade fallout
// that depends on them. Blocked resources are further ordered by how deep they
// sit in the dependency chain — a resource appears below whatever it is blocked
// by — and then alphabetically.
func sortedResources(resources []FailingResource) []FailingResource {
	sorted := slices.Clone(resources)
	depth := blockedDepths(sorted)

	slices.SortFunc(sorted, func(left, right FailingResource) int {
		if sl, sr := left.state(), right.state(); sl != sr {
			return int(sl) - int(sr)
		}

		if left.state() == stateBlocked {
			if dl, dr := depth[left.displayName()], depth[right.displayName()]; dl != dr {
				return dl - dr
			}
		}

		return strings.Compare(left.displayName(), right.displayName())
	})

	return sorted
}

// blockedDepths returns, for each blocked resource (keyed by displayName), the
// number of blocked ancestors above it in the dependency chain (0 = blocked
// directly by a root/non-blocked resource). Keying by displayName and matching
// edges via dependencyKey keeps same-named resources in different namespaces
// distinct. The walk is bounded by the number of blocked resources so a
// dependency cycle cannot loop forever.
func blockedDepths(resources []FailingResource) map[string]int {
	blocker := make(map[string]string)
	blocked := make(map[string]bool)

	for _, res := range resources {
		if res.state() == stateBlocked {
			blocked[res.displayName()] = true
			blocker[res.displayName()] = dependencyKey(res.Message)
		}
	}

	depth := make(map[string]int, len(blocked))

	for name := range blocked {
		steps, cur := 0, name

		for range len(blocked) {
			parent := blocker[cur]
			if parent == "" || !blocked[parent] {
				break
			}

			steps++
			cur = parent
		}

		depth[name] = steps
	}

	return depth
}

// formatSummary builds the summary string from the root causes and the number
// of blocked dependents.
func formatSummary(roots []FailingResource, blocked int) string {
	switch len(roots) {
	case 0:
		if blocked > 0 {
			return "reconciliation failed: " + pluralizeResources(blocked) + " not ready"
		}

		return "reconciliation failed — see diagnostics above"
	case 1:
		msg := "reconciliation failed: " + roots[0].displayName()
		if reason := roots[0].Reason; reason != "" {
			msg += " (" + reason + ")"
		}

		return msg + blockedSuffix(blocked)
	default:
		names := make([]string, len(roots))
		for i, res := range roots {
			names[i] = res.displayName()
		}

		return fmt.Sprintf(
			"reconciliation failed: %s not ready (%s)%s",
			pluralizeResources(len(roots)), strings.Join(names, ", "), blockedSuffix(blocked),
		)
	}
}

// pluralizeResources returns "1 resource" or "N resources" with correct grammar.
func pluralizeResources(count int) string {
	if count == 1 {
		return "1 resource"
	}

	return fmt.Sprintf("%d resources", count)
}

// blockedSuffix returns the trailing "; N dependent(s) blocked" clause, or "".
func blockedSuffix(blocked int) string {
	if blocked <= 0 {
		return ""
	}

	if blocked == 1 {
		return "; 1 dependent blocked"
	}

	return fmt.Sprintf("; %d dependents blocked", blocked)
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
