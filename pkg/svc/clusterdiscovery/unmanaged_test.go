package clusterdiscovery_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/clusterdiscovery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeKubeconfig writes a minimal kubeconfig containing the given context names to a temp file and
// returns its path. Only the contexts matter for DiscoverUnmanaged (it reads config.Contexts).
func writeKubeconfig(t *testing.T, contextNames ...string) string {
	t.Helper()

	var builder strings.Builder

	builder.WriteString("apiVersion: v1\nkind: Config\nclusters:\n")

	for _, name := range contextNames {
		fmt.Fprintf(&builder,
			"  - name: %s\n    cluster:\n      server: https://127.0.0.1:6443\n", name)
	}

	builder.WriteString("contexts:\n")

	for _, name := range contextNames {
		fmt.Fprintf(&builder,
			"  - name: %s\n    context:\n      cluster: %s\n      user: %s\n", name, name, name)
	}

	builder.WriteString("users:\n")

	for _, name := range contextNames {
		fmt.Fprintf(&builder, "  - name: %s\n    user: {}\n", name)
	}

	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(path, []byte(builder.String()), 0o600))

	return path
}

func TestDiscoverUnmanaged_SurfacesUnmanagedContext(t *testing.T) {
	t.Parallel()

	path := writeKubeconfig(t, "some-external-cluster")

	got := clusterdiscovery.DiscoverUnmanaged(path, map[string]struct{}{})

	require.Len(t, got, 1)
	assert.Equal(t, "some-external-cluster", got[0].Name)
	assert.Equal(t, clusterdiscovery.RunStateUnmanaged, got[0].RunState)
	assert.Empty(t, got[0].Provider, "an unmanaged cluster has no provider")
	assert.Empty(t, got[0].Distribution, "an unmanaged cluster has no distribution")
}

func TestDiscoverUnmanaged_SkipsManagedByRawContextName(t *testing.T) {
	t.Parallel()

	// A context whose raw name is already a managed cluster must not be re-surfaced.
	path := writeKubeconfig(t, "prod")

	got := clusterdiscovery.DiscoverUnmanaged(path, map[string]struct{}{"prod": {}})

	assert.Empty(t, got)
}

func TestDiscoverUnmanaged_SkipsManagedByDetectedName(t *testing.T) {
	t.Parallel()

	// "kind-dev" detects to the ksail name "dev" — the key discovery uses for the managed cluster —
	// so it must be deduped even though the raw context name differs.
	path := writeKubeconfig(t, "kind-dev")

	got := clusterdiscovery.DiscoverUnmanaged(path, map[string]struct{}{"dev": {}})

	assert.Empty(t, got)
}

func TestDiscoverUnmanaged_MixedContextsSortedAndDeduped(t *testing.T) {
	t.Parallel()

	// "kind-managed" detects to "managed" (deduped); the other two are unrecognized context names
	// (unmanaged) and must come back sorted.
	path := writeKubeconfig(t, "zeta-external", "kind-managed", "alpha-external")

	got := clusterdiscovery.DiscoverUnmanaged(path, map[string]struct{}{"managed": {}})

	names := make([]string, len(got))
	for i, cluster := range got {
		names[i] = cluster.Name
	}

	assert.Equal(t, []string{"alpha-external", "zeta-external"}, names)
}

func TestDiscoverUnmanaged_UnreadableKubeconfigYieldsNoneNoError(t *testing.T) {
	t.Parallel()

	// A missing kubeconfig is best-effort: no unmanaged clusters, no panic, no error surface.
	got := clusterdiscovery.DiscoverUnmanaged(
		filepath.Join(t.TempDir(), "does-not-exist"),
		map[string]struct{}{},
	)

	assert.Empty(t, got)
}

// alwaysUnmanaged is an isManaged predicate that treats every context as unmanaged.
func alwaysUnmanaged(string) bool { return false }

func TestUnmanagedContextNames_NilConfigYieldsNone(t *testing.T) {
	t.Parallel()

	assert.Empty(t, clusterdiscovery.UnmanagedContextNames(nil, alwaysUnmanaged))
}

func TestUnmanagedContextNames_SortedAndDeduped(t *testing.T) {
	t.Parallel()

	config := clusterdiscovery.LoadKubeconfig(
		writeKubeconfig(t, "zeta-external", "alpha-external", "beta-external"))
	require.NotNil(t, config)

	got := clusterdiscovery.UnmanagedContextNames(config, alwaysUnmanaged)

	assert.Equal(t, []string{"alpha-external", "beta-external", "zeta-external"}, got)
}

func TestUnmanagedContextNames_SkipsManagedByRawName(t *testing.T) {
	t.Parallel()

	config := clusterdiscovery.LoadKubeconfig(writeKubeconfig(t, "managed-ctx", "external-ctx"))
	require.NotNil(t, config)

	got := clusterdiscovery.UnmanagedContextNames(config, func(name string) bool {
		return name == "managed-ctx"
	})

	assert.Equal(t, []string{"external-ctx"}, got)
}
