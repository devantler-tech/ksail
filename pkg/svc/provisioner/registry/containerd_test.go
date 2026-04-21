package registry_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- EscapeShellArg ---

//nolint:funlen // Table-driven test coverage is naturally long.
func TestEscapeShellArg(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		arg  string
		want string
	}{
		{
			name: "simple string",
			arg:  "hello",
			want: "'hello'",
		},
		{
			name: "string with single quote",
			arg:  "it's",
			want: "'it'\\''s'",
		},
		{
			name: "string with multiple single quotes",
			arg:  "it's a 'test'",
			want: "'it'\\''s a '\\''test'\\'''",
		},
		{
			name: "empty string",
			arg:  "",
			want: "''",
		},
		{
			name: "string with spaces",
			arg:  "hello world",
			want: "'hello world'",
		},
		{
			name: "string with double quotes",
			arg:  `say "hello"`,
			want: `'say "hello"'`,
		},
		{
			name: "string with special characters",
			arg:  "/etc/containerd/certs.d/docker.io",
			want: "'/etc/containerd/certs.d/docker.io'",
		},
		{
			name: "string with newlines",
			arg:  "line1\nline2",
			want: "'line1\nline2'",
		},
		{
			name: "string with dollar sign",
			arg:  "$HOME/path",
			want: "'$HOME/path'",
		},
		{
			name: "string with backticks",
			arg:  "`whoami`",
			want: "'`whoami`'",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := registry.EscapeShellArg(tc.arg)
			assert.Equal(t, tc.want, got)
		})
	}
}

// --- GenerateRandomDelimiter ---

func TestGenerateRandomDelimiter(t *testing.T) {
	t.Parallel()

	t.Run("starts with EOF_ prefix", func(t *testing.T) {
		t.Parallel()

		delimiter, err := registry.GenerateRandomDelimiter()
		require.NoError(t, err)
		assert.True(
			t,
			strings.HasPrefix(delimiter, "EOF_"),
			"expected prefix EOF_, got %s",
			delimiter,
		)
	})

	t.Run("has expected length", func(t *testing.T) {
		t.Parallel()

		delimiter, err := registry.GenerateRandomDelimiter()
		require.NoError(t, err)
		// "EOF_" (4 chars) + 16 hex chars = 20 chars total
		assert.Len(t, delimiter, 20)
	})

	t.Run("generates unique delimiters", func(t *testing.T) {
		t.Parallel()

		seen := make(map[string]struct{})

		const iterations = 100

		for range iterations {
			delimiter, err := registry.GenerateRandomDelimiter()
			require.NoError(t, err)

			_, duplicate := seen[delimiter]
			assert.False(t, duplicate, "duplicate delimiter generated: %s", delimiter)
			seen[delimiter] = struct{}{}
		}
	})

	t.Run("contains only valid hex characters after prefix", func(t *testing.T) {
		t.Parallel()

		delimiter, err := registry.GenerateRandomDelimiter()
		require.NoError(t, err)

		hexPart := delimiter[4:] // strip "EOF_"
		for _, c := range hexPart {
			isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
			assert.True(t, isHex, "non-hex character %c in delimiter hex part", c)
		}
	})
}
