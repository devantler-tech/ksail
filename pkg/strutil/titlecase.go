package strutil

import "strings"

// SnakeCaseToTitle converts a snake_case or space-separated string to Title Case.
// Underscores and spaces are both treated as word separators.
// Examples: "cluster_create" → "Cluster Create", "workload read" → "Workload Read".
func SnakeCaseToTitle(s string) string {
	s = strings.ReplaceAll(s, "_", " ")
	words := strings.Fields(s)

	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}

	return strings.Join(words, " ")
}
