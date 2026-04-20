package sops_test

import (
	"testing"

	sopsclient "github.com/devantler-tech/ksail/v7/pkg/client/sops"
	"github.com/getsops/sops/v3/stores/json"
	"github.com/getsops/sops/v3/stores/yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:funlen // Table-driven test coverage is naturally long.
func TestGetStores(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		inputPath string
		wantErr   bool
		storeType string // "yaml" or "json"
	}{
		{
			name:      "yaml extension",
			inputPath: "/path/to/file.yaml",
			storeType: "yaml",
		},
		{
			name:      "yml extension",
			inputPath: "/path/to/file.yml",
			storeType: "yaml",
		},
		{
			name:      "json extension",
			inputPath: "/path/to/file.json",
			storeType: "json",
		},
		{
			name:      "unsupported extension returns error",
			inputPath: "/path/to/file.toml",
			wantErr:   true,
		},
		{
			name:      "no extension returns error",
			inputPath: "/path/to/file",
			wantErr:   true,
		},
		{
			name:      "txt extension returns error",
			inputPath: "/path/to/file.txt",
			wantErr:   true,
		},
		{
			name:      "xml extension returns error",
			inputPath: "config.xml",
			wantErr:   true,
		},
	}

	//nolint:varnamelen // Short names keep table-driven tests readable.
	for i := range tests {
		tc := tests[i]

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			inputStore, outputStore, err := sopsclient.GetStores(tc.inputPath)

			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, sopsclient.ErrUnsupportedFileFormat)
				assert.Nil(t, inputStore)
				assert.Nil(t, outputStore)
			} else {
				require.NoError(t, err)
				require.NotNil(t, inputStore)
				require.NotNil(t, outputStore)

				switch tc.storeType {
				case "yaml":
					assert.IsType(t, &yaml.Store{}, inputStore)
					assert.IsType(t, &yaml.Store{}, outputStore)
				case "json":
					assert.IsType(t, &json.Store{}, inputStore)
					assert.IsType(t, &json.Store{}, outputStore)
				}
			}
		})
	}
}

func TestGetDecryptStores(t *testing.T) {
	t.Parallel()

	t.Run("stdin defaults to yaml", func(t *testing.T) {
		t.Parallel()

		inputStore, outputStore, err := sopsclient.GetDecryptStores("", true)

		require.NoError(t, err)
		assert.IsType(t, &yaml.Store{}, inputStore)
		assert.IsType(t, &yaml.Store{}, outputStore)
	})

	t.Run("file path with json extension", func(t *testing.T) {
		t.Parallel()

		inputStore, outputStore, err := sopsclient.GetDecryptStores("/path/to/file.json", false)

		require.NoError(t, err)
		assert.IsType(t, &json.Store{}, inputStore)
		assert.IsType(t, &json.Store{}, outputStore)
	})

	t.Run("file path with yaml extension", func(t *testing.T) {
		t.Parallel()

		inputStore, outputStore, err := sopsclient.GetDecryptStores("/path/to/file.yaml", false)

		require.NoError(t, err)
		assert.IsType(t, &yaml.Store{}, inputStore)
		assert.IsType(t, &yaml.Store{}, outputStore)
	})

	t.Run("unsupported extension without stdin", func(t *testing.T) {
		t.Parallel()

		_, _, err := sopsclient.GetDecryptStores("/path/to/file.toml", false)

		require.Error(t, err)
	})
}
