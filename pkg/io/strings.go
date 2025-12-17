package io

import "strings"

// String manipulation helpers.

// TrimNonEmpty returns the trimmed string and whether it's non-empty.
// This consolidates the common pattern of trimming and checking for emptiness.
//
// Parameters:
//   - s: The string to trim and check
//
// Returns:
//   - string: The trimmed string
//   - bool: True if the trimmed string is non-empty, false otherwise
func TrimNonEmpty(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)

	return trimmed, trimmed != ""
}
