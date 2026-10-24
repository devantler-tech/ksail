package ciharness_test

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/mirrorregistry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// defaultMirrorGap is a registry CI allows that KSail's default mirrors do not cover.
type defaultMirrorGap struct {
	// host is the registry host, as a system-test image names it.
	host string
	// direct is true when CI pulls the registry straight from upstream (its DIRECT list)
	// rather than through a mirror only CI creates (its MIRRORED list).
	direct bool
	// reason says why the gap exists and, where one does, names the issue that closes it.
	reason string
}

// knownDefaultMirrorGaps lists every registry CI allows beyond KSail's default mirrors.
//
// A user who creates a local Docker cluster with the default mirrors pulls each of these
// straight from upstream: not cached across cluster recreations, not available offline,
// and subject to the registry's anonymous quota. CI going green says nothing about that,
// so every such registry is recorded here with its reason. Close a gap by adding the
// registry to the default mirrors and deleting its entry.
func knownDefaultMirrorGaps() []defaultMirrorGap {
	return []defaultMirrorGap{
		{
			host:   "ecr-public.aws.com",
			reason: "Argo CD's Redis image; CI mirrors it but the default mirrors do not yet (#7336)",
		},
		{
			host:   "reg.kyverno.io",
			direct: true,
			reason: "Kyverno's own registry; CI pulls it directly because no anonymous quota has " +
				"failed a system test, and no default mirror covers it",
		},
	}
}

// Where CI declares the registries it allows. Each is a shell list assignment inside the
// named step of a composite action.
const (
	warmMirrorCacheAction = ".github/actions/warm-mirror-cache/action.yaml"
	mirrorCoverageStep    = "🧭 Check every image registry has a mirror"
	ksailClusterAction    = ".github/actions/ksail-cluster/action.yml"
	mirrorVolumesStep     = "🪞 Resolve mirror registry volumes"
)

// TestCIRegistryListsMatchDefaultMirrorsPlusKnownGaps keeps the registries CI allows tied to
// the mirrors users get by default (ksail#7668).
//
// CI's mirror check fails a system-test image whose registry is in neither its MIRRORED nor
// its DIRECT list, and ksail-cluster falls back to its own list of mirror hosts. Nothing tied
// those lists to KSail's default mirrors, so widening them made CI pass for images users
// still pull from upstream: that is how ksail#7336 went unnoticed after #7333 added the ECR
// Public mirror to CI only.
func TestCIRegistryListsMatchDefaultMirrorsPlusKnownGaps(t *testing.T) {
	t.Parallel()

	defaults := defaultMirrorHosts(t)
	wantMirrored, wantDirect := expectedCIRegistries(defaults, knownDefaultMirrorGaps())

	coverage := findHarnessStep(
		t, readCompositeAction(t, warmMirrorCacheAction).Runs.Steps, mirrorCoverageStep,
	).Run
	volumes := findHarnessStep(
		t, readCompositeAction(t, ksailClusterAction).Runs.Steps, mirrorVolumesStep,
	).Run

	for _, list := range []struct {
		name string
		got  []string
		want []string
	}{
		{"warm-mirror-cache MIRRORED", shellListAssignment(t, coverage, "MIRRORED"), wantMirrored},
		{"warm-mirror-cache DIRECT", shellListAssignment(t, coverage, "DIRECT"), wantDirect},
		{"ksail-cluster fallback mirror hosts", shellListAssignment(t, volumes, "hosts"), wantMirrored},
	} {
		assert.Empty(t, registryListDrift(list.name, list.got, list.want))
	}
}

// TestKnownDefaultMirrorGapsAreStillGaps keeps the recorded gaps honest: a gap a default
// mirror already covers is closed and must be removed, and every gap says why it exists.
func TestKnownDefaultMirrorGapsAreStillGaps(t *testing.T) {
	t.Parallel()

	defaults := defaultMirrorHosts(t)
	seen := map[string]bool{}

	for _, gap := range knownDefaultMirrorGaps() {
		assert.NotContainsf(t, defaults, gap.host,
			"%s is covered by a default mirror, so the gap is closed: delete its entry "+
				"from knownDefaultMirrorGaps", gap.host)
		assert.NotEmptyf(t, strings.TrimSpace(gap.reason),
			"recorded gap %s must say why it exists", gap.host)
		assert.Falsef(t, seen[gap.host], "gap %s is recorded twice", gap.host)

		seen[gap.host] = true
	}
}

// TestRegistryListDriftNamesBothFixes proves the comparison can fail, and that its message
// tells the reader both ways to resolve the drift.
func TestRegistryListDriftNamesBothFixes(t *testing.T) {
	t.Parallel()

	want := []string{"docker.io", "ghcr.io"}

	assert.Empty(t, registryListDrift("list", []string{"ghcr.io", "docker.io"}, want),
		"the same registries in another order are not drift")

	for _, got := range [][]string{
		{"docker.io", "ghcr.io", "example.com"},
		{"docker.io"},
	} {
		drift := registryListDrift("list", got, want)
		require.NotEmptyf(t, drift, "%v differs from %v and must be reported", got, want)
		assert.Contains(t, drift, "mirrorregistry.DefaultMirrors")
		assert.Contains(t, drift, "knownDefaultMirrorGaps")
	}
}

// defaultMirrorHosts returns the registry host of every default mirror.
func defaultMirrorHosts(t *testing.T) []string {
	t.Helper()

	hosts := make([]string, 0, len(mirrorregistry.DefaultMirrors))
	for _, spec := range mirrorregistry.DefaultMirrors {
		host, _, _ := strings.Cut(spec, "=")
		hosts = append(hosts, host)
	}

	require.Contains(t, hosts, "docker.io",
		"default mirror discovery must find the Docker Hub mirror KSail is known to create")

	return hosts
}

// expectedCIRegistries returns what CI's MIRRORED and DIRECT lists must hold: every default
// mirror plus the recorded mirrored gaps, and exactly the recorded direct gaps.
func expectedCIRegistries(defaults []string, gaps []defaultMirrorGap) ([]string, []string) {
	mirrored := slices.Clone(defaults)

	var direct []string

	for _, gap := range gaps {
		if gap.direct {
			direct = append(direct, gap.host)
		} else {
			mirrored = append(mirrored, gap.host)
		}
	}

	return mirrored, direct
}

// registryListDrift compares a CI registry list with what it must hold, ignoring order, and
// returns a message naming both fixes when they differ, or "" when they match.
func registryListDrift(name string, got, want []string) string {
	got = sortedUnique(got)
	want = sortedUnique(want)

	if slices.Equal(got, want) {
		return ""
	}

	return fmt.Sprintf(
		"%s holds %v but must hold %v: KSail's default mirrors plus the gaps recorded in "+
			"knownDefaultMirrorGaps. For a registry CI now allows, either add it to "+
			"mirrorregistry.DefaultMirrors so users get the mirror too, or record it in "+
			"knownDefaultMirrorGaps with its reason and the issue that closes it",
		name, got, want,
	)
}

func sortedUnique(values []string) []string {
	sorted := slices.Clone(values)
	slices.Sort(sorted)

	return slices.Compact(sorted)
}

// shellListAssignment returns the words of the single non-empty, literal assignment
// variable="..." in script. An assignment that expands a variable is not a literal list and
// is skipped, so a list built at runtime is never mistaken for the declared one.
func shellListAssignment(t *testing.T, script, variable string) []string {
	t.Helper()

	assignment := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(variable) + `="([^"$]+)"\s*$`)
	matches := assignment.FindAllStringSubmatch(script, -1)
	require.Lenf(
		t,
		matches,
		1,
		"expected exactly one literal %s=\"...\" list in the step, found %d",
		variable,
		len(matches),
	)

	words := strings.Fields(matches[0][1])
	require.NotEmptyf(t, words, "the %s list must name at least one registry", variable)

	return words
}
