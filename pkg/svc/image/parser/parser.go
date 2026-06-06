package parser

import (
	"fmt"
	"regexp"

	"gopkg.in/yaml.v3"
)

// minMatchCount is the minimum number of regex matches required to extract the image reference.
const minMatchCount = 2

// fromDirectiveRe matches all FROM directives in a Dockerfile, including
// multi-stage builds and optional flags (e.g. --platform).
//

var fromDirectiveRe = regexp.MustCompile(`(?m)^FROM\s+(?:--\S+\s+)*([^\s]+)`)

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

// ParseChartVersionFromChartYaml extracts the pinned version of the named Helm chart
// dependency from an embedded Chart.yaml. This keeps Go code in sync with Dependabot's
// helm ecosystem automatically, used for charts whose version diverges from their
// app/image tag and therefore cannot be tracked via a Dockerfile FROM directive.
// Panics if the document cannot be parsed or the named dependency is absent - this
// catches embedding/format issues at init time.
func ParseChartVersionFromChartYaml(chartYAMLContent, dependencyName string) string {
	var chart struct {
		Dependencies []struct {
			Name    string `yaml:"name"`
			Version string `yaml:"version"`
		} `yaml:"dependencies"`
	}

	err := yaml.Unmarshal([]byte(chartYAMLContent), &chart)
	if err != nil {
		panic(
			fmt.Sprintf(
				"failed to parse embedded Chart.yaml for %s dependency: %v",
				dependencyName,
				err,
			),
		)
	}

	for _, dep := range chart.Dependencies {
		if dep.Name == dependencyName {
			return dep.Version
		}
	}

	panic(
		fmt.Sprintf(
			"failed to find %s dependency version in embedded Chart.yaml - "+
				"check that Chart.yaml exists and lists the dependency",
			dependencyName,
		),
	)
}

// ParseAllImagesFromDockerfile extracts all container image references from FROM
// directives in a Dockerfile. Returns a slice of image references exactly as they
// appear in the FROM directives (which may be qualified or unqualified). This is
// useful for Dockerfiles that track multiple related images (e.g., Flux distribution
// controller images).
func ParseAllImagesFromDockerfile(dockerfileContent string) []string {
	matches := fromDirectiveRe.FindAllStringSubmatch(dockerfileContent, -1)

	images := make([]string, 0, len(matches))

	for _, m := range matches {
		if len(m) >= minMatchCount {
			images = append(images, m[1])
		}
	}

	return images
}
