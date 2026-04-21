package vcluster_test

import (
	"regexp"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/vcluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChartVersion verifies that ChartVersion returns a valid semantic version.
func TestChartVersion(t *testing.T) {
	t.Parallel()

	version := vcluster.ChartVersion()

	require.NotEmpty(t, version, "ChartVersion should return a non-empty version")
	assert.Regexp(
		t,
		`^\d+\.\d+\.\d+`,
		version,
		"ChartVersion should return a valid semantic version",
	)
}

// TestChartVersion_MatchesDockerfileFormat verifies the chart version matches expected format from Dockerfile.
func TestChartVersion_MatchesDockerfileFormat(t *testing.T) {
	t.Parallel()

	version := vcluster.ChartVersion()

	// The chart version is extracted from the Dockerfile line:
	// FROM ghcr.io/loft-sh/vcluster-pro:0.33.1
	// Expected format: semver with optional pre-release suffix
	assert.Regexp(t,
		`^\d+\.\d+\.\d+(-[a-zA-Z0-9.-]+)?$`,
		version,
		"ChartVersion should match Dockerfile format (semver with optional pre-release)",
	)
}

// TestDefaultKubernetesVersion verifies that the default Kubernetes version is set.
func TestDefaultKubernetesVersion(t *testing.T) {
	t.Parallel()

	version := vcluster.DefaultKubernetesVersion

	require.NotEmpty(t, version, "DefaultKubernetesVersion should be non-empty")
	assert.Regexp(
		t,
		`^v\d+\.\d+\.\d+`,
		version,
		"DefaultKubernetesVersion should start with 'v' followed by semver",
	)
}

// TestDefaultKubernetesVersion_MatchesDockerfileFormat verifies the K8s version matches expected Dockerfile format.
func TestDefaultKubernetesVersion_MatchesDockerfileFormat(t *testing.T) {
	t.Parallel()

	version := vcluster.DefaultKubernetesVersion

	// The Kubernetes version is extracted from the Dockerfile line:
	// FROM ghcr.io/loft-sh/kubernetes:v1.35.3
	// Expected format: v-prefixed semver with optional suffix
	assert.Regexp(
		t,
		`^v\d+\.\d+\.\d+(-[a-zA-Z0-9.-]+)?$`,
		version,
		"DefaultKubernetesVersion should match Dockerfile format (v-prefixed semver with optional suffix)",
	)
}

// TestDefaultKubernetesVersion_MajorMinorExtraction verifies major.minor can be extracted from the version.
func TestDefaultKubernetesVersion_MajorMinorExtraction(t *testing.T) {
	t.Parallel()

	version := vcluster.DefaultKubernetesVersion

	// Extract major.minor from the version (e.g., "1.32" from "v1.32.3")
	re := regexp.MustCompile(`^v(\d+\.\d+)\.\d+`)
	matches := re.FindStringSubmatch(version)
	require.Len(t, matches, 2, "should extract major.minor from version")
	assert.NotEmpty(t, matches[1], "major.minor should not be empty")
}

// TestChartVersion_Stability verifies the chart version remains stable across multiple calls.
func TestChartVersion_Stability(t *testing.T) {
	t.Parallel()

	version1 := vcluster.ChartVersion()
	version2 := vcluster.ChartVersion()

	assert.Equal(
		t,
		version1,
		version2,
		"ChartVersion should return the same value across multiple calls",
	)
}

// TestDefaultKubernetesVersion_Stability verifies the K8s version remains stable across multiple reads.
func TestDefaultKubernetesVersion_Stability(t *testing.T) {
	t.Parallel()

	version1 := vcluster.DefaultKubernetesVersion
	version2 := vcluster.DefaultKubernetesVersion

	assert.Equal(
		t,
		version1,
		version2,
		"DefaultKubernetesVersion should be stable across multiple reads",
	)
}

// TestChartVersion_ExpectedValue verifies the chart version matches the current Dockerfile content.
func TestChartVersion_ExpectedValue(t *testing.T) {
	t.Parallel()

	version := vcluster.ChartVersion()

	// This test documents the current Dockerfile content.
	// Dependabot is configured to update this image but may not track it (dependabot-core#13383);
	// update manually if needed.
	// FROM ghcr.io/loft-sh/vcluster-pro:0.33.1
	assert.Equal(
		t,
		"0.33.1",
		version,
		"ChartVersion should match current Dockerfile (update this test when manually bumping the version)",
	)
}

// TestDefaultKubernetesVersion_ExpectedValue verifies the K8s version matches the current Dockerfile content.
func TestDefaultKubernetesVersion_ExpectedValue(t *testing.T) {
	t.Parallel()

	version := vcluster.DefaultKubernetesVersion

	// This test documents the current Dockerfile content.
	// Dependabot is configured to update this image but may not track it (dependabot-core#13383);
	// update manually if needed.
	// FROM ghcr.io/loft-sh/kubernetes:v1.35.3
	assert.Equal(
		t,
		"v1.35.3",
		version,
		"DefaultKubernetesVersion should match current Dockerfile (update this test when manually bumping the version)",
	)
}
