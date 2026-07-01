package hubble

import "strings"

// FilterOptions narrows a set of observed flows. A zero value keeps every flow.
type FilterOptions struct {
	// Namespace keeps flows whose source or destination namespace matches
	// exactly. Empty means "any namespace".
	Namespace string
	// Pod keeps flows whose source or destination pod name contains this
	// substring (pod names usually carry a generated suffix). Empty means
	// "any pod".
	Pod string
	// Protocol keeps flows of this L4 protocol, matched case-insensitively
	// (for example "tcp"). Empty means "any protocol".
	Protocol string
}

// FilterFlows returns the records that satisfy every set field of opts,
// preserving the input order. It never mutates the input slice.
func FilterFlows(records []FlowRecord, opts FilterOptions) []FlowRecord {
	if opts == (FilterOptions{}) {
		return records
	}

	filtered := make([]FlowRecord, 0, len(records))

	for _, record := range records {
		if record.matches(opts) {
			filtered = append(filtered, record)
		}
	}

	return filtered
}

// matches reports whether a single record satisfies every set filter field.
func (r FlowRecord) matches(opts FilterOptions) bool {
	return r.matchesNamespace(opts.Namespace) &&
		r.matchesPod(opts.Pod) &&
		r.matchesProtocol(opts.Protocol)
}

func (r FlowRecord) matchesNamespace(namespace string) bool {
	if namespace == "" {
		return true
	}

	return r.Source.Namespace == namespace || r.Destination.Namespace == namespace
}

func (r FlowRecord) matchesPod(pod string) bool {
	if pod == "" {
		return true
	}

	return strings.Contains(r.Source.Pod, pod) || strings.Contains(r.Destination.Pod, pod)
}

func (r FlowRecord) matchesProtocol(protocol string) bool {
	if protocol == "" {
		return true
	}

	return strings.EqualFold(r.Protocol, protocol)
}
