package registry

import "strings"

// TrimNonEmpty returns the trimmed string and whether it's non-empty.
// This consolidates the common pattern of trimming and checking for emptiness.
func TrimNonEmpty(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)

	return trimmed, trimmed != ""
}
