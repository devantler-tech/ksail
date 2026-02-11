package k8s

import "strings"

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
