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

// SanitizeToDNSLabel converts an arbitrary string to a lowercase alphanumeric
// string with hyphens as the only separator. Consecutive hyphens are collapsed
// and leading/trailing hyphens are trimmed.
//
// This is the shared sanitization kernel used by OCI repository segment
// normalisation and DNS-1123 repo name construction.
func SanitizeToDNSLabel(value string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return ""
	}

	var builder strings.Builder

	prevHyphen := false

	for _, char := range trimmed {
		switch {
		case (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9'):
			builder.WriteRune(char)

			prevHyphen = false
		default:
			if !prevHyphen {
				builder.WriteRune('-')

				prevHyphen = true
			}
		}
	}

	return strings.Trim(builder.String(), "-")
}
