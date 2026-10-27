package fluxsubst_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/fluxsubst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yannh/kubeconform/pkg/cache"
)

// TestExpandFluxSubstitutionsReadsCurrentCache catches parsed schemas and misses
// surviving a validation invocation after the on-disk schema has been refreshed.
func TestExpandFluxSubstitutionsReadsCurrentCache(t *testing.T) {
	cases := []struct {
		name, kind, filename, initialType, before, nextType, after string
	}{
		{
			name: "changed schema is reloaded", kind: "CacheRefreshChanged",
			filename: "cacherefreshchanged_v1.json", initialType: "integer",
			before: "size: 0", nextType: "boolean", after: "size: true",
		},
		{
			name: "previously absent schema becomes available", kind: "CacheRefreshMissing",
			filename: "cacherefreshmissing_v1.json",
			before:   "size: placeholder", nextType: "integer", after: "size: 0",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			home := t.TempDir()
			for _, key := range []string{"HOME", "USERPROFILE", "XDG_CACHE_HOME", "LOCALAPPDATA"} {
				t.Setenv(key, home)
			}

			cacheDir := fluxsubst.SchemaCacheDir()
			require.NoError(t, os.MkdirAll(cacheDir, 0o700))

			url := "https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/" +
				"refresh.ksail.io/" + testCase.filename
			store := cache.NewOnDiskCache(cacheDir)

			writeSchema := func(schemaType string) {
				t.Helper()

				schema := fmt.Sprintf(
					`{"properties":{"spec":{"properties":{"size":{"type":%q}}}}}`,
					schemaType,
				)
				require.NoError(t, store.Set(url, []byte(schema)))
			}
			if testCase.initialType != "" {
				writeSchema(testCase.initialType)
			}

			input := []byte(
				"apiVersion: refresh.ksail.io/v1\nkind: " + testCase.kind +
					"\nspec:\n  size: ${SIZE}\n",
			)
			assert.Contains(t, string(fluxsubst.ExpandFluxSubstitutions(input)), testCase.before)

			// Model a successful explicit refresh through kubeconform's native cache.
			// Every file here is owned by this case's private temporary cache.
			entries, err := os.ReadDir(cacheDir)
			require.NoError(t, err)

			for _, entry := range entries {
				require.NoError(t, os.Remove(filepath.Join(cacheDir, entry.Name())))
			}

			writeSchema(testCase.nextType)
			assert.Contains(t, string(fluxsubst.ExpandFluxSubstitutions(input)), testCase.after)
		})
	}
}
