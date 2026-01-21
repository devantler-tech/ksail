package k3d

import (
	_ "embed"
	"fmt"
	"regexp"
)

// minMatchCount is the minimum number of regex matches required to extract the image reference.
const minMatchCount = 2

// Embedded Dockerfile containing image references (updated by Dependabot).
//
//go:embed Dockerfile
var dockerfile string

// parseImageFromDockerfile extracts a container image reference from a Dockerfile using the provided regex pattern.
// This ensures Go code stays in sync with Dependabot updates automatically.
// Panics if the Dockerfile cannot be parsed - this catches embedding/format issues at init time.
func parseImageFromDockerfile(dockerfileContent, pattern, imageName string) string {
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

// k3sImage returns the K3s container image reference from the embedded Dockerfile.
func k3sImage() string {
	return parseImageFromDockerfile(dockerfile, `FROM\s+(rancher/k3s:[^\s]+)`, "K3s")
}
