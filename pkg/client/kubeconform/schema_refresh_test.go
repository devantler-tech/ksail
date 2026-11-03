package kubeconform_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/kubeconform"
	"github.com/stretchr/testify/require"
	"github.com/yannh/kubeconform/pkg/cache"
)

const (
	refreshTestURL      = "https://schemas.example.invalid/widget.json"
	invalidRefreshEntry = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
)

// TestSchemaRefreshRefusesUnexpectedEntries catches partial promotion or reading
// a symlink/directory in place of a native cache file. Rejection preserves old bytes.
//
//nolint:paralleltest // seedSchemaCache owns process-wide cache environment variables.
func TestSchemaRefreshRefusesUnexpectedEntries(t *testing.T) {
	for _, kind := range []string{"unknown-name", "directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			seedSchemaCache(t, map[string]string{refreshTestURL: "original"})

			stable := kubeconform.DefaultSchemaCacheDir()
			refresh, err := kubeconform.NewSchemaRefresh()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, refresh.Close()) })
			require.NoError(
				t,
				cache.NewOnDiskCache(refresh.CacheDirectory()).Set(refreshTestURL, []byte("fresh")),
			)

			badName := invalidRefreshEntry
			outside := filepath.Join(t.TempDir(), "outside")
			require.NoError(t, os.WriteFile(outside, []byte("untouched"), 0o600))

			switch kind {
			case "unknown-name":
				badName = "zzz-unexpected-entry"
				require.NoError(
					t,
					os.WriteFile(
						filepath.Join(refresh.CacheDirectory(), badName),
						[]byte("other"),
						0o600,
					),
				)
			case "directory":
				require.NoError(
					t,
					os.Mkdir(filepath.Join(refresh.CacheDirectory(), badName), 0o700),
				)
			case "symlink":
				require.NoError(
					t,
					os.Symlink(outside, filepath.Join(refresh.CacheDirectory(), badName)),
				)
			}

			require.Error(
				t,
				refresh.Commit(),
				"every input must be checked before any persistent entry changes",
			)

			value, err := cache.NewOnDiskCache(stable).Get(refreshTestURL)
			require.NoError(t, err)
			require.Equal(t, []byte("original"), value)

			_, err = os.Lstat(filepath.Join(stable, badName))
			require.ErrorIs(t, err, os.ErrNotExist)
			//nolint:gosec // outside is an owned t.TempDir fixture, not caller input.
			unchanged, err := os.ReadFile(outside)
			require.NoError(t, err)
			require.Equal(t, "untouched", string(unchanged))
		})
	}
}

// TestSchemaRefreshReplacesOnlyItsDownloadedEntry catches accidental deletion
// of unrelated cache data and verifies native readers see the promoted response.
//
//nolint:paralleltest // seedSchemaCache owns process-wide cache environment variables.
func TestSchemaRefreshReplacesOnlyItsDownloadedEntry(t *testing.T) {
	seedSchemaCache(t, map[string]string{refreshTestURL: "original"})

	stable := kubeconform.DefaultSchemaCacheDir()
	unrelated := filepath.Join(stable, "unrelated-note")
	require.NoError(t, os.WriteFile(unrelated, []byte("retain"), 0o600))

	refresh, err := kubeconform.NewSchemaRefresh()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, refresh.Close()) })
	require.NoError(
		t,
		cache.NewOnDiskCache(refresh.CacheDirectory()).Set(refreshTestURL, []byte("fresh")),
	)
	require.NoError(t, refresh.Commit())

	value, err := cache.NewOnDiskCache(stable).Get(refreshTestURL)
	require.NoError(t, err)
	require.Equal(t, []byte("fresh"), value)

	//nolint:gosec // unrelated is a named file in this test's isolated cache.
	retained, err := os.ReadFile(unrelated)
	require.NoError(t, err)
	require.Equal(t, "retain", string(retained))
	require.NoError(t, refresh.Close())
	_, err = os.Stat(refresh.CacheDirectory())
	require.ErrorIs(t, err, os.ErrNotExist)
}
