// Package labels provides utility functions for working with Kubernetes-style label maps.
package labels

import "slices"

// UniqueValues extracts the unique non-empty values for a given key from a slice of labeled items.
// The getLabels function extracts the label map from each item.
func UniqueValues[T any](items []T, key string, getLabels func(T) map[string]string) []string {
	seen := make(map[string]struct{})

	for _, item := range items {
		if v, ok := getLabels(item)[key]; ok && v != "" {
			seen[v] = struct{}{}
		}
	}

	result := make([]string, 0, len(seen))
	for v := range seen {
		result = append(result, v)
	}

	slices.Sort(result)

	return result
}
