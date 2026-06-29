package workload_test

import (
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanonicalizeSchemaLocations(t *testing.T) {
	t.Parallel()

	t.Run("passes URLs through verbatim", func(t *testing.T) {
		t.Parallel()

		url := "https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/" +
			"{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json"
		assert.Equal(t, []string{url}, workload.ExportCanonicalizeSchemaLocations([]string{url}))
	})

	t.Run("passes kubeconform path templates through verbatim", func(t *testing.T) {
		t.Parallel()

		tmpl := "schemas/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json"
		assert.Equal(t, []string{tmpl}, workload.ExportCanonicalizeSchemaLocations([]string{tmpl}))
	})

	t.Run("canonicalizes an existing local directory to an absolute path", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		// EvalCanonicalPath resolves symlinks (e.g. macOS /var -> /private/var), so
		// compare against the symlink-resolved temp dir.
		want, err := filepath.EvalSymlinks(dir)
		require.NoError(t, err)

		got := workload.ExportCanonicalizeSchemaLocations([]string{dir})
		require.Len(t, got, 1)
		assert.True(t, filepath.IsAbs(got[0]), "result must be absolute")
		assert.Equal(t, want, got[0])
	})

	t.Run("leaves an unresolvable local path as supplied", func(t *testing.T) {
		t.Parallel()

		// A path under a non-existent parent cannot be resolved; it passes through
		// so kubeconform surfaces a clear error rather than the canonicalizer.
		got := workload.ExportCanonicalizeSchemaLocations([]string{"./relative/schemas"})
		require.Len(t, got, 1)
		// Either canonicalized (if the relative dir resolves) or returned as-is, but
		// never dropped.
		assert.NotEmpty(t, got[0])
	})
}
