// Package envvar provides utilities for working with environment variables.
package envvar

import (
	"os"
	"regexp"
)

// pattern matches ${VAR_NAME} placeholders for environment variable expansion.
var pattern = regexp.MustCompile(`\$\{([a-zA-Z_][a-zA-Z0-9_]*)\}`)

// Expand replaces ${VAR_NAME} placeholders with their environment variable values.
// If a referenced environment variable is not set, the placeholder is replaced with an empty string.
func Expand(value string) string {
	if value == "" {
		return value
	}

	return pattern.ReplaceAllStringFunc(value, func(match string) string {
		// Extract variable name from ${VAR_NAME}
		varName := match[2 : len(match)-1]

		return os.Getenv(varName)
	})
}
