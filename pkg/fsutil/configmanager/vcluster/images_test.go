package vcluster_test

import (
	_ "embed"
	"regexp"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/vcluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dockerfileForTest is an independent embed of the package's Dockerfile so the
// "MatchesDockerfile" tests below can parse it without going through the same parser
// the production code uses. Reusing the production parser would make those tests a
// tautology; parsing the file independently catches drift in the production regex or
// in the Dockerfile format.
//
//go:embed Dockerfile
var dockerfileForTest string

// parseImageTagFromDockerfile is a deliberately independent implementation (simple line
// scan + string ops, NOT regex) that extracts the tag for the given image repo from the
// embedded Dockerfile. Keeping this distinct from the production regex-based parser is
// the whole point of the MatchesDockerfile tests — see dockerfileForTest's comment.
func parseImageTagFromDockerfile(t *testing.T, repo string) string {
	t.Helper()

	const fromPrefix = "FROM "

	prefix := repo + ":"

	for raw := range strings.SplitSeq(dockerfileForTest, "\n") {
		line := strings.TrimSpace(raw)

		ref, ok := strings.CutPrefix(line, fromPrefix)
		if !ok {
			continue
		}

		// Strip optional @sha256:... digest suffix so we get the tag alone.
		if i := strings.Index(ref, "@"); i >= 0 {
			ref = ref[:i]
		}

		if tag, ok := strings.CutPrefix(ref, prefix); ok {
			return tag
		}
	}

	t.Fatalf("Dockerfile contains no FROM line for repo %q", repo)

	return ""
}

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
	// FROM ghcr.io/loft-sh/vcluster-pro:<tag>
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
	// FROM ghcr.io/loft-sh/kubernetes:<tag>
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

// TestChartVersion_MatchesDockerfile verifies the production parser's chart version matches
// the Dockerfile, parsed independently here. This is a tripwire for drift in the production
// parser (regex bug, Dockerfile format change) and — unlike a hardcoded expected value — it
// does NOT need manual updating when Dependabot bumps the Dockerfile. Dependabot does not
// update test code (see dependabot-core#13383), so a hardcoded expected value turns every
// bump into a red main branch; this dynamic check avoids that without losing the tripwire.
func TestChartVersion_MatchesDockerfile(t *testing.T) {
	t.Parallel()

	expected := parseImageTagFromDockerfile(t, "ghcr.io/loft-sh/vcluster-pro")

	assert.Equal(
		t,
		expected,
		vcluster.ChartVersion(),
		"ChartVersion() must match the tag in the Dockerfile (production parser drift?)",
	)
}

// TestDefaultKubernetesVersion_MatchesDockerfile verifies the production parser's Kubernetes
// version matches the Dockerfile, parsed independently here. See the longer comment on
// TestChartVersion_MatchesDockerfile for the rationale (in short: tripwire that survives
// Dependabot bumps).
func TestDefaultKubernetesVersion_MatchesDockerfile(t *testing.T) {
	t.Parallel()

	expected := parseImageTagFromDockerfile(t, "ghcr.io/loft-sh/kubernetes")

	assert.Equal(
		t,
		expected,
		vcluster.DefaultKubernetesVersion,
		"DefaultKubernetesVersion must match the tag in the Dockerfile (production parser drift?)",
	)
}
