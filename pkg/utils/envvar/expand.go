// Package envvar provides utilities for working with environment variables.
package envvar

import (
	"log/slog"
	"os"
	"regexp"
)

// pattern matches ${VAR_NAME} and ${VAR_NAME:-default} placeholders for environment variable expansion.
// Groups: 1 = variable name, 2 = optional default value (after :-).
var pattern = regexp.MustCompile(`\$\{([a-zA-Z_][a-zA-Z0-9_]*)(?::-([^}]*))?\}`)

// minGroupsForVarName is the minimum number of regex groups required to extract a variable name.
const minGroupsForVarName = 2

// Expand replaces ${VAR_NAME} and ${VAR_NAME:-default} placeholders with their environment variable values.
// If a referenced environment variable is not set:
//   - With default syntax ${VAR:-default}: uses the default value
//   - Without default ${VAR}: uses empty string and logs a warning
func Expand(value string) string {
	if value == "" {
		return value
	}

	return pattern.ReplaceAllStringFunc(value, func(match string) string {
		// Use FindStringSubmatch to extract groups
		groups := pattern.FindStringSubmatch(match)
		if len(groups) < minGroupsForVarName {
			return match // Should not happen with valid regex
		}

		varName := groups[1]
		envValue, exists := os.LookupEnv(varName)

		if exists {
			return envValue
		}

		// Variable not set - check for default value
		if len(groups) > 2 && groups[2] != "" {
			// Has explicit default value (non-empty)
			return groups[2]
		}

		// Check if default syntax was used (even with empty default)
		if len(match) > len(varName)+4 && match[len(varName)+2:len(varName)+4] == ":-" {
			// Empty default specified: ${VAR:-}
			return ""
		}

		// No default - warn and return empty string
		slog.Warn("environment variable not set", "variable", varName)

		return ""
	})
}

// ExpandBytes expands environment variables in byte slice content.
// This is a convenience wrapper for expanding YAML or other file content.
func ExpandBytes(data []byte) []byte {
	return []byte(Expand(string(data)))
}
