package sopsutil_test

import (
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/sopsutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testAgeKey = "AGE-SECRET-KEY-1TESTKEY000000000000000000000000000000000000000000000000"
	metaAgeKey = "AGE-SECRET-KEY-1METAKEY000000000000000000000000000000000000000000000000"
)

// TestExtractAgeKey verifies extraction of AGE-SECRET-KEY lines from various inputs.
func TestExtractAgeKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "single key line",
			input: "AGE-SECRET-KEY-1ABCDEF0000000000000000000000000000000000000000000000000",
			want:  "AGE-SECRET-KEY-1ABCDEF0000000000000000000000000000000000000000000000000",
		},
		{
			name: "key with comment metadata",
			input: "# created: 2024-01-01T00:00:00Z\n# public key: age1abc123\n" +
				"AGE-SECRET-KEY-1ABCDEF0000000000000000000000000000000000000000000000000\n",
			want: "AGE-SECRET-KEY-1ABCDEF0000000000000000000000000000000000000000000000000",
		},
		{
			name: "multiple keys returns first",
			input: "AGE-SECRET-KEY-FIRST000000000000000000000000000000000000000000000000\n" +
				"AGE-SECRET-KEY-SECOND00000000000000000000000000000000000000000000000",
			want: "AGE-SECRET-KEY-FIRST000000000000000000000000000000000000000000000000",
		},
		{
			name:  "key with surrounding whitespace is trimmed",
			input: "  AGE-SECRET-KEY-1ABCDEF0000000000000000000000000000000000000000000000000  ",
			want:  "AGE-SECRET-KEY-1ABCDEF0000000000000000000000000000000000000000000000000",
		},
		{
			name:  "empty input returns empty",
			input: "",
			want:  "",
		},
		{
			name:  "no age key returns empty",
			input: "some random data\nno key here",
			want:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := sopsutil.ExtractAgeKey(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func noKeyFileEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SOPS_AGE_KEY_FILE", filepath.Join(t.TempDir(), "keys.txt"))
}

// TestResolveEnabledAgeKey verifies all branches of the enable/disable/auto-detect logic.
//
//nolint:paralleltest // Uses t.Setenv
func TestResolveEnabledAgeKey(t *testing.T) {
	t.Run("explicitly disabled returns empty without error", func(t *testing.T) {
		disabled := false
		sops := v1alpha1.SOPS{Enabled: &disabled}
		got, err := sopsutil.ResolveEnabledAgeKey(sops)
		require.NoError(t, err)
		assert.Empty(t, got)
	})
	t.Run("auto-detect with no key returns empty without error", func(t *testing.T) {
		t.Setenv("TEST_SOPSUTIL_NONEXISTENT_VAR_AUTO_11111", "")
		noKeyFileEnv(t)

		sops := v1alpha1.SOPS{AgeKeyEnvVar: "TEST_SOPSUTIL_NONEXISTENT_VAR_AUTO_11111"}
		got, err := sopsutil.ResolveEnabledAgeKey(sops)
		require.NoError(t, err)
		assert.Empty(t, got)
	})
	t.Run("auto-detect with env key returns key", func(t *testing.T) {
		t.Setenv("TEST_SOPSUTIL_AGE_KEY_AUTO_22222", testAgeKey)
		noKeyFileEnv(t)

		sops := v1alpha1.SOPS{AgeKeyEnvVar: "TEST_SOPSUTIL_AGE_KEY_AUTO_22222"}
		got, err := sopsutil.ResolveEnabledAgeKey(sops)
		require.NoError(t, err)
		assert.Equal(t, testAgeKey, got)
	})
	t.Run("auto-detect with metadata env key extracts key", func(t *testing.T) {
		t.Setenv("TEST_SOPSUTIL_AGE_KEY_META_33333", "# comment\n"+metaAgeKey+"\n")
		noKeyFileEnv(t)

		sops := v1alpha1.SOPS{AgeKeyEnvVar: "TEST_SOPSUTIL_AGE_KEY_META_33333"}
		got, err := sopsutil.ResolveEnabledAgeKey(sops)
		require.NoError(t, err)
		assert.Equal(t, metaAgeKey, got)
	})
	t.Run("explicitly enabled with no key returns error", func(t *testing.T) {
		t.Setenv("TEST_SOPSUTIL_NONEXISTENT_VAR_ENABLED_44444", "")
		noKeyFileEnv(t)

		enabled := true
		sops := v1alpha1.SOPS{
			Enabled:      &enabled,
			AgeKeyEnvVar: "TEST_SOPSUTIL_NONEXISTENT_VAR_ENABLED_44444",
		}
		got, err := sopsutil.ResolveEnabledAgeKey(sops)
		require.ErrorIs(t, err, sopsutil.ErrSOPSKeyNotFound)
		assert.Empty(t, got)
	})
	t.Run("explicitly enabled with env key returns key", func(t *testing.T) {
		t.Setenv("TEST_SOPSUTIL_AGE_KEY_ENABLED_55555", testAgeKey)
		noKeyFileEnv(t)

		enabled := true
		sops := v1alpha1.SOPS{
			Enabled:      &enabled,
			AgeKeyEnvVar: "TEST_SOPSUTIL_AGE_KEY_ENABLED_55555",
		}
		got, err := sopsutil.ResolveEnabledAgeKey(sops)
		require.NoError(t, err)
		assert.Equal(t, testAgeKey, got)
	})
}
