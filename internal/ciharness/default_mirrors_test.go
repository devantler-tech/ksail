package ciharness_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/mirrorregistry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mirroredHostsAssignment matches the MIRRORED list in the warm-mirror-cache coverage check.
var mirroredHostsAssignment = regexp.MustCompile(`(?m)^\s*MIRRORED="([^"]*)"`)

// defaultClusterHostsAssignment matches the mirror hosts ksail-cluster assumes when it passes
// no --mirror-registry, i.e. when KSail falls back to its default mirrors.
var defaultClusterHostsAssignment = regexp.MustCompile(`(?m)^\s*hosts="([^"$]+)"`)

// TestImageRegistryCoverageCheckUsesKSailDefaultMirrors ties CI's image-registry coverage check
// to KSail's own default mirrors (ksail#7336).
//
// warm-mirror-cache lists the images of every system-test component configuration with
// `ksail workload images` and fails when one comes from a registry outside its MIRRORED set. That
// check is the test that a default component image comes from a registry a default mirror covers
// only while MIRRORED is exactly KSail's default mirror set. Before ksail#7336, MIRRORED carried
// ECR Public for Argo CD's Redis while KSail's defaults did not, so CI pulled Redis through a
// mirror that users never got. ksail-cluster's fallback host list, used when no mirror flag is
// passed, must name the same defaults or its cache import and cleanup miss a mirror.
func TestImageRegistryCoverageCheckUsesKSailDefaultMirrors(t *testing.T) {
	t.Parallel()

	defaults := defaultMirrorHosts(t)

	warm := readCompositeAction(t, ".github/actions/warm-mirror-cache/action.yaml")
	coverage := findHarnessStep(t, warm.Runs.Steps, "🧭 Check every image registry has a mirror")
	mirrored := mirroredHostsAssignment.FindStringSubmatch(coverage.Run)
	require.Len(t, mirrored, 2, "the coverage check must assign MIRRORED")
	assert.ElementsMatch(t, defaults, strings.Fields(mirrored[1]),
		"MIRRORED in warm-mirror-cache must equal KSail's DefaultMirrors hosts")

	cluster := readCompositeAction(t, ".github/actions/ksail-cluster/action.yml")
	volumes := findHarnessStep(t, cluster.Runs.Steps, "🪞 Resolve mirror registry volumes")
	fallback := defaultClusterHostsAssignment.FindStringSubmatch(volumes.Run)
	require.Len(t, fallback, 2, "ksail-cluster must name the default mirror hosts")
	assert.ElementsMatch(t, defaults, strings.Fields(fallback[1]),
		"ksail-cluster's fallback mirror hosts must equal KSail's DefaultMirrors hosts")
}

// defaultMirrorHosts returns the registry host of every KSail default mirror spec.
func defaultMirrorHosts(t *testing.T) []string {
	t.Helper()

	hosts := make([]string, 0, len(mirrorregistry.DefaultMirrors))

	for _, spec := range mirrorregistry.DefaultMirrors {
		host, _, found := strings.Cut(spec, "=")
		require.True(t, found, "default mirror %q must name an upstream", spec)

		hosts = append(hosts, host)
	}

	return hosts
}
