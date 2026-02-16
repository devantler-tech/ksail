package vcluster

import (
	_ "embed"
	"regexp"
)

// Embedded Dockerfile containing image references (updated by Dependabot).
//
//go:embed Dockerfile
var dockerfile string

// versionRegex extracts the tag from the vCluster Kubernetes image.
var versionRegex = regexp.MustCompile(`FROM\s+ghcr\.io/loft-sh/kubernetes:([^\s]+)`)

// minMatchCount is the minimum number of regex matches required.
const minMatchCount = 2

// kubernetesVersion returns the Kubernetes version tag from the embedded Dockerfile.
// This ensures Go code stays in sync with Dependabot updates automatically.
// Panics if the Dockerfile cannot be parsed - this catches embedding/format issues at init time.
func kubernetesVersion() string {
	matches := versionRegex.FindStringSubmatch(dockerfile)
	if len(matches) < minMatchCount {
		panic(
			"failed to parse vCluster Kubernetes version from embedded Dockerfile - " +
				"check that the Dockerfile exists and contains a valid FROM directive",
		)
	}

	return matches[1]
}
