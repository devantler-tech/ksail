package talos

import (
	_ "embed"
	"regexp"
)

// Embedded Dockerfile containing image references (updated by Dependabot).
//
//go:embed Dockerfile
var dockerfile string

// talosImage returns the Talos container image reference from the embedded Dockerfile.
// This ensures Go code stays in sync with Dependabot updates automatically.
// Panics if the Dockerfile cannot be parsed - this catches embedding/format issues at init time.
func talosImage() string {
	re := regexp.MustCompile(`FROM\s+(ghcr\.io/siderolabs/talos:[^\s]+)`)
	matches := re.FindStringSubmatch(dockerfile)

	if len(matches) < 2 {
		panic("failed to parse Talos image from embedded Dockerfile - check that the Dockerfile exists and contains a valid FROM directive")
	}

	return matches[1]
}
