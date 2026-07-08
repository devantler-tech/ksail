// Package sliceutil carries the small slice helpers shared by the bootstrap
// config renderers, so the distributions normalise rendered lists identically
// instead of each carrying its own copy.
package sliceutil

import "sort"

// SortedNonEmpty returns a sorted copy of values with empty entries dropped, so
// a rendered list is deterministic and free of blank items. It never mutates
// the caller's slice and returns nil for an all-empty input so the enclosing
// key is omitted from the rendered output.
func SortedNonEmpty(values []string) []string {
	out := make([]string, 0, len(values))

	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}

	if len(out) == 0 {
		return nil
	}

	sort.Strings(out)

	return out
}

// ValidateAndPrealloc runs validate and, on success, returns a freshly allocated slice of the
// given capacity — the validate-then-preallocate prelude shared by every distribution's Plan
// (k3s, kubeadm) before they diverge on how they populate the plan's nodes.
func ValidateAndPrealloc[N any](validate func() error, capacity int) ([]N, error) {
	err := validate()
	if err != nil {
		return nil, err
	}

	return make([]N, 0, capacity), nil
}
