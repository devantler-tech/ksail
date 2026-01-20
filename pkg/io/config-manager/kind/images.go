package kind

import (
	_ "embed"
	"regexp"
)

// minMatchCount is the minimum number of regex matches required to extract the image reference.
const minMatchCount = 2

// Embedded Dockerfile containing image references (updated by Dependabot).
//
//go:embed Dockerfile
var dockerfile string

// kindNodeImage returns the Kind node container image reference from the embedded Dockerfile.
// This ensures Go code stays in sync with Dependabot updates automatically.
// Panics if the Dockerfile cannot be parsed - this catches embedding/format issues at init time.
func kindNodeImage() string {
	re := regexp.MustCompile(`FROM\s+(kindest/node:[^\s]+)`)
	matches := re.FindStringSubmatch(dockerfile)

	if len(matches) < minMatchCount {
		panic(
			"failed to parse Kind node image from embedded Dockerfile - " +
				"check that the Dockerfile exists and contains a valid FROM directive",
		)
	}

	return matches[1]
}
