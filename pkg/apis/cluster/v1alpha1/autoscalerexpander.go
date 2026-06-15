package v1alpha1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// AutoscalerExpander defines the node expander strategy for the cluster autoscaler.
type AutoscalerExpander string

const (
	// AutoscalerExpanderPrice selects the node group with the lowest cost.
	AutoscalerExpanderPrice AutoscalerExpander = "Price"
	// AutoscalerExpanderLeastWaste selects the node group that will have the least idle CPU/memory.
	AutoscalerExpanderLeastWaste AutoscalerExpander = "LeastWaste"
	// AutoscalerExpanderLeastNodes selects the node group that will result in the fewest total nodes.
	AutoscalerExpanderLeastNodes AutoscalerExpander = "LeastNodes"
	// AutoscalerExpanderRandom selects a node group at random.
	AutoscalerExpanderRandom AutoscalerExpander = "Random"
)

// Set for AutoscalerExpander (pflag.Value interface).
func (a *AutoscalerExpander) Set(value string) error {
	for _, v := range ValidAutoscalerExpanders() {
		if strings.EqualFold(value, string(v)) {
			*a = v

			return nil
		}
	}

	return fmt.Errorf(
		"%w: %s (valid options: %s, %s, %s, %s)",
		ErrInvalidAutoscalerExpander,
		value,
		AutoscalerExpanderPrice,
		AutoscalerExpanderLeastWaste,
		AutoscalerExpanderLeastNodes,
		AutoscalerExpanderRandom,
	)
}

// String returns the string representation of the AutoscalerExpander.
func (a *AutoscalerExpander) String() string {
	return string(*a)
}

// Type returns the type of the AutoscalerExpander.
func (a *AutoscalerExpander) Type() string {
	return "AutoscalerExpander"
}

// Default returns the default value for AutoscalerExpander (LeastWaste).
func (a *AutoscalerExpander) Default() any {
	return AutoscalerExpanderLeastWaste
}

// ValidValues returns all valid AutoscalerExpander values as strings.
func (a *AutoscalerExpander) ValidValues() []string {
	return []string{
		string(AutoscalerExpanderPrice),
		string(AutoscalerExpanderLeastWaste),
		string(AutoscalerExpanderLeastNodes),
		string(AutoscalerExpanderRandom),
	}
}

// AutoscalerExpanderList is an ordered priority list of expander strategies for
// the cluster autoscaler. The autoscaler applies them as a chain: the first
// expander filters candidate node groups and each subsequent expander breaks the
// ties left by the previous one. This mirrors the upstream cluster-autoscaler
// priority-list form (e.g. --expander=least-nodes,least-waste).
//
// In ksail.yaml the field accepts either a single scalar value
// (expander: LeastWaste) or a YAML sequence (expander: [LeastNodes, LeastWaste]);
// the configuration loader normalises a scalar into a single-element list.
type AutoscalerExpanderList []AutoscalerExpander

// String returns the comma-separated representation of the list, matching the
// upstream cluster-autoscaler --expander priority-list format.
func (l AutoscalerExpanderList) String() string {
	parts := make([]string, len(l))
	for i, expander := range l {
		parts[i] = string(expander)
	}

	return strings.Join(parts, ",")
}

// SplitAutoscalerExpanders splits a scalar expander value into its individual
// entries, trimming surrounding whitespace from each. A comma-separated scalar
// ("LeastNodes,LeastWaste") yields one entry per element, mirroring the upstream
// cluster-autoscaler --expander priority-list syntax; an empty or whitespace-only
// value yields an empty (non-nil) slice. It is the shared normaliser for the JSON
// unmarshaler (legacy persisted scalar form) and the YAML configuration decode
// hook, so both accept identical scalar inputs.
func SplitAutoscalerExpanders(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}

	parts := strings.Split(raw, ",")
	expanders := make([]string, 0, len(parts))

	for _, part := range parts {
		expanders = append(expanders, strings.TrimSpace(part))
	}

	return expanders
}

// UnmarshalJSON decodes an AutoscalerExpanderList from JSON, accepting both the
// current priority-list form (["LeastNodes","LeastWaste"]) and the legacy scalar
// form ("LeastWaste") that older ksail versions persisted to cluster state
// (~/.ksail/clusters/<name>/spec.json) before this field became a list. A
// comma-separated scalar ("LeastNodes,LeastWaste") is split into its entries,
// matching the YAML configuration decode hook and the upstream cluster-autoscaler
// --expander syntax. This keeps state files written before the migration readable
// after an upgrade.
func (l *AutoscalerExpanderList) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)

	// JSON null leaves the list unchanged (idiomatic no-op).
	if string(trimmed) == "null" {
		return nil
	}

	// Current form: a JSON array of expander values. Decode into the underlying
	// slice type to avoid recursing back into this method.
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var list []AutoscalerExpander

		err := json.Unmarshal(data, &list)
		if err != nil {
			return fmt.Errorf("unmarshal autoscaler expander list: %w", err)
		}

		*l = list

		return nil
	}

	// Legacy form: a single (optionally comma-separated) scalar string.
	var raw string

	err := json.Unmarshal(data, &raw)
	if err != nil {
		return fmt.Errorf("unmarshal autoscaler expander scalar: %w", err)
	}

	parts := SplitAutoscalerExpanders(raw)
	expanders := make(AutoscalerExpanderList, len(parts))

	for index, part := range parts {
		expanders[index] = AutoscalerExpander(part)
	}

	*l = expanders

	return nil
}
