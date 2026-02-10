package envvar

import (
	"log/slog"
	"os"
	"regexp"
	"strings"
)

// pattern matches ${VAR_NAME} and ${VAR_NAME:-default} placeholders for environment variable expansion.
// Groups: 1 = variable name, 2 = optional default value (after :-).
var pattern = regexp.MustCompile(`\$\{([a-zA-Z_][a-zA-Z0-9_]*)(?::-([^}]*))?\}`)

// minGroupsForVarName is the minimum number of regex groups required to extract a variable name.
const minGroupsForVarName = 2

// defaultSyntaxMarker is the delimiter used for default value syntax in env var placeholders.
const defaultSyntaxMarker = ":-"

// Expand replaces ${VAR_NAME} and ${VAR_NAME:-default} placeholders with their environment variable values.
// If a referenced environment variable is not set:
//   - With default syntax ${VAR:-default}: uses the default value
//   - Without default ${VAR}: uses empty string and logs a warning
func Expand(value string) string {
	if value == "" {
		return value
	}

	return pattern.ReplaceAllStringFunc(value, expandMatch)
}

// expandMatch handles expansion of a single regex match.
func expandMatch(match string) string {
	groups := pattern.FindStringSubmatch(match)
	if len(groups) < minGroupsForVarName {
		return match // Should not happen with valid regex
	}

	varName := groups[1]
	envValue, exists := os.LookupEnv(varName)

	if exists {
		return envValue
	}

	return resolveDefault(match, groups)
}

// resolveDefault returns the appropriate value when an env var is not set.
// It checks if default syntax was used and either returns the default value,
// an empty string (for explicit empty default), or logs a warning.
func resolveDefault(match string, groups []string) string {
	varName := groups[1]

	// Check for non-empty default value
	if len(groups) > 2 && groups[2] != "" {
		return groups[2]
	}

	// Check if default syntax was used (handles ${VAR:-} with empty default)
	if strings.Contains(match, defaultSyntaxMarker) {
		return ""
	}

	// No default syntax - warn and return empty string
	slog.Warn("environment variable not set", "variable", varName)

	return ""
}

// ExpandBytes expands environment variables in byte slice content.
// This is a convenience wrapper for expanding YAML or other file content.
func ExpandBytes(data []byte) []byte {
	return []byte(Expand(string(data)))
}
