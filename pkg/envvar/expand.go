package envvar

import (
	"log/slog"
	"os"
	"regexp"
	"strings"
)

// pattern matches ${VAR_NAME}, ${VAR_NAME:-default}, and ${VAR_NAME:=default}
// placeholders for variable expansion.
// Groups: 1 = variable name, 2 = optional operator (:- or :=), 3 = optional default value.
var pattern = regexp.MustCompile(`\$\{([a-zA-Z_][a-zA-Z0-9_]*)(?:(:-|:=)([^}]*))?\}`)

// minGroupsForVarName is the minimum number of regex groups required to extract a variable name.
const minGroupsForVarName = 2

// defaultSyntaxMarkers are the delimiters used for default value syntax in variable placeholders.
var defaultSyntaxMarkers = []string{":-", ":="}

// Expand replaces ${VAR_NAME}, ${VAR_NAME:-default}, and ${VAR_NAME:=default}
// placeholders with their environment variable values.
// If a referenced environment variable is not set:
//   - With default syntax ${VAR:-default} or ${VAR:=default}: uses the default value
//   - Without default ${VAR}: uses empty string and logs a warning
func Expand(value string) string {
	return ExpandWithLookup(value, os.LookupEnv)
}

// ExpandWithLookup expands placeholders using the provided lookup function.
// The lookup function is checked before default handling.
func ExpandWithLookup(value string, lookup func(string) (string, bool)) string {
	if value == "" {
		return value
	}

	return pattern.ReplaceAllStringFunc(value, func(match string) string {
		return expandMatch(match, lookup)
	})
}

// expandMatch handles expansion of a single regex match.
func expandMatch(match string, lookup func(string) (string, bool)) string {
	groups := pattern.FindStringSubmatch(match)
	if len(groups) < minGroupsForVarName {
		return match // Should not happen with valid regex
	}

	varName := groups[1]
	envValue, exists := lookup(varName)

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
	if len(groups) > 3 && groups[3] != "" {
		return groups[3]
	}

	// Check if default syntax was used (handles ${VAR:-} and ${VAR:=} with empty default)
	for _, marker := range defaultSyntaxMarkers {
		if strings.Contains(match, marker) {
			return ""
		}
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

// ExpandBytesWithLookup expands placeholders in byte slice content using the provided lookup function.
func ExpandBytesWithLookup(data []byte, lookup func(string) (string, bool)) []byte {
	return []byte(ExpandWithLookup(string(data), lookup))
}
