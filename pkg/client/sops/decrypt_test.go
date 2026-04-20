package sops_test

import (
	"testing"

	sopsclient "github.com/devantler-tech/ksail/v7/pkg/client/sops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:funlen // Table-driven test coverage is naturally long.
func TestParseExtractPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		extract string
		want    []any
		wantErr bool
	}{
		{
			name:    "single key",
			extract: `["data"]`,
			want:    []any{"data"},
		},
		{
			name:    "nested keys",
			extract: `["data"]["password"]`,
			want:    []any{"data", "password"},
		},
		{
			name:    "triple nested keys",
			extract: `["a"]["b"]["c"]`,
			want:    []any{"a", "b", "c"},
		},
		{
			name:    "single quoted outer",
			extract: `'["key"]'`,
			want:    []any{"key"},
		},
		{
			name:    "double quoted outer",
			extract: `"["key"]"`,
			want:    []any{"key"},
		},
		{
			name:    "empty brackets returns error",
			extract: `[]`,
			wantErr: true,
		},
		{
			name:    "empty string returns error",
			extract: "",
			wantErr: true,
		},
		{
			name:    "keys with single quotes inside brackets",
			extract: `['key1']['key2']`,
			want:    []any{"key1", "key2"},
		},
	}

	//nolint:varnamelen // Short names keep table-driven tests readable.
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := sopsclient.ParseExtractPath(tc.extract)

			if tc.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, sopsclient.ErrInvalidExtractPath)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.want, got)
			}
		})
	}
}

func TestHandleEmitError(t *testing.T) {
	t.Parallel()

	t.Run("nil error returns data", func(t *testing.T) {
		t.Parallel()

		data := []byte("decrypted content")
		got, err := sopsclient.HandleEmitError(nil, data)

		require.NoError(t, err)
		assert.Equal(t, data, got)
	})

	t.Run("nil error with nil data", func(t *testing.T) {
		t.Parallel()

		got, err := sopsclient.HandleEmitError(nil, nil)

		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("generic error wraps as dump error", func(t *testing.T) {
		t.Parallel()

		genericErr := assert.AnError

		got, err := sopsclient.HandleEmitError(genericErr, []byte("data"))

		require.Error(t, err)
		assert.Nil(t, got)
		assert.Contains(t, err.Error(), "error dumping file")
	})
}
