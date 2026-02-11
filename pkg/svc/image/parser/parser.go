package parser

import (
	"fmt"
	"regexp"
)

// minMatchCount is the minimum number of regex matches required to extract the image reference.
const minMatchCount = 2

// ParseImageFromDockerfile extracts a container image reference from a Dockerfile using the provided regex pattern.
// This ensures Go code stays in sync with Dependabot updates automatically.
// Panics if the Dockerfile cannot be parsed - this catches embedding/format issues at init time.
func ParseImageFromDockerfile(dockerfileContent, pattern, imageName string) string {
	re := regexp.MustCompile(pattern)
	matches := re.FindStringSubmatch(dockerfileContent)

	if len(matches) < minMatchCount {
		panic(
			fmt.Sprintf(
				"failed to parse %s image from embedded Dockerfile - "+
					"check that the Dockerfile exists and contains a valid FROM directive",
				imageName,
			),
		)
	}

	return matches[1]
}
