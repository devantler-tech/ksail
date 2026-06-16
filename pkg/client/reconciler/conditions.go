package reconciler

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Condition holds the standard status-condition fields shared by Flux and
// ArgoCD custom resources. It is the parsed form of a single entry in a CR's
// status.conditions slice.
type Condition struct {
	// Type is the condition type (e.g. "Ready", "Stalled").
	Type string
	// Status is the condition status (e.g. "True", "False").
	Status string
	// Reason is the machine-readable reason for the condition's last transition.
	Reason string
	// Message is the human-readable message for the condition.
	Message string
}

// ParseConditions extracts the status.conditions slice from an unstructured
// GitOps custom resource and returns the parsed conditions. Entries that are
// not condition maps are skipped. It returns nil when the object carries no
// conditions.
func ParseConditions(obj *unstructured.Unstructured) []Condition {
	raw, found, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if !found || len(raw) == 0 {
		return nil
	}

	conditions := make([]Condition, 0, len(raw))

	for _, entry := range raw {
		cond, ok := ParseCondition(entry)
		if !ok {
			continue
		}

		conditions = append(conditions, cond)
	}

	return conditions
}

// ParseCondition extracts the standard condition fields from a single raw
// condition entry. It returns the parsed condition and true when the entry is a
// condition map, or the zero Condition and false otherwise.
func ParseCondition(entry any) (Condition, bool) {
	condMap, ok := entry.(map[string]any)
	if !ok {
		return Condition{}, false
	}

	condType, _, _ := unstructured.NestedString(condMap, "type")
	condStatus, _, _ := unstructured.NestedString(condMap, "status")
	condReason, _, _ := unstructured.NestedString(condMap, "reason")
	condMessage, _, _ := unstructured.NestedString(condMap, "message")

	return Condition{
		Type:    condType,
		Status:  condStatus,
		Reason:  condReason,
		Message: condMessage,
	}, true
}
