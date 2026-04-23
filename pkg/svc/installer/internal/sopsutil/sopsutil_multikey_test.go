package sopsutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/sopsutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ExtractAllAgeKeys
// ---------------------------------------------------------------------------

func TestExtractAllAgeKeys(t *testing.T) {
	t.Parallel()

	const (
		key1 = "AGE-SECRET-KEY-1ABCDEF0000000000000000000000000000000000000000000000000"
		key2 = "AGE-SECRET-KEY-FIRST000000000000000000000000000000000000000000000000"
		key3 = "AGE-SECRET-KEY-SECOND00000000000000000000000000000000000000000000000"
		key4 = "AGE-SECRET-KEY-THIRD000000000000000000000000000000000000000000000000"
	)

	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"single key", key1, []string{key1}},
		{
			"multiple keys with metadata",
			"# comment\n" + key2 + "\n# comment\n" + key3 + "\n",
			[]string{key2, key3},
		},
		{"three keys", key2 + "\n" + key3 + "\n" + key4 + "\n", []string{key2, key3, key4}},
		{"empty input", "", nil},
		{"no keys", "# just comments\n# nothing here", nil},
		{"key with whitespace trimmed", "  " + key1 + "  ", []string{key1}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := sopsutil.ExtractAllAgeKeys(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// FilterKeysByPublicKeys
// ---------------------------------------------------------------------------

func TestFilterKeysByPublicKeys_EmptyInputs(t *testing.T) {
	t.Parallel()

	t.Run("empty private keys returns empty", func(t *testing.T) {
		t.Parallel()

		result, err := sopsutil.FilterKeysByPublicKeys(nil, []string{"age1test"})
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("empty public keys returns all private keys", func(t *testing.T) {
		t.Parallel()

		keys := []string{"AGE-SECRET-KEY-1ABCDEF0000000000000000000000000000000000000000000000000"}
		result, err := sopsutil.FilterKeysByPublicKeys(keys, nil)
		require.NoError(t, err)
		assert.Equal(t, keys, result)
	})
}

func TestFilterKeysByPublicKeys_Matching(t *testing.T) {
	t.Parallel()

	identity, err := age.GenerateX25519Identity()
	require.NoError(t, err)

	privKey := identity.String()
	pubKey := identity.Recipient().String()

	result, filterErr := sopsutil.FilterKeysByPublicKeys(
		[]string{privKey},
		[]string{pubKey},
	)
	require.NoError(t, filterErr)
	require.Len(t, result, 1)
	assert.Equal(t, privKey, result[0])
}

func TestFilterKeysByPublicKeys_NonMatching(t *testing.T) {
	t.Parallel()

	identity, err := age.GenerateX25519Identity()
	require.NoError(t, err)

	other, err := age.GenerateX25519Identity()
	require.NoError(t, err)

	result, filterErr := sopsutil.FilterKeysByPublicKeys(
		[]string{identity.String()},
		[]string{other.Recipient().String()},
	)
	require.NoError(t, filterErr)
	assert.Empty(t, result)
}

func TestFilterKeysByPublicKeys_MixedKeys(t *testing.T) {
	t.Parallel()

	id1, err := age.GenerateX25519Identity()
	require.NoError(t, err)

	id2, err := age.GenerateX25519Identity()
	require.NoError(t, err)

	id3, err := age.GenerateX25519Identity()
	require.NoError(t, err)

	// Only include public key for id1 and id3, not id2.
	result, filterErr := sopsutil.FilterKeysByPublicKeys(
		[]string{id1.String(), id2.String(), id3.String()},
		[]string{id1.Recipient().String(), id3.Recipient().String()},
	)
	require.NoError(t, filterErr)
	require.Len(t, result, 2)
	assert.Equal(t, id1.String(), result[0])
	assert.Equal(t, id3.String(), result[1])
}

// ---------------------------------------------------------------------------
// ResolveAgeKey: env.var override
// ---------------------------------------------------------------------------

func TestResolveAgeKey_EnvVarOverride(t *testing.T) {
	const testKey = "AGE-SECRET-KEY-1ENVVAR00000000000000000000000000000000000000000000000000"

	t.Run("env.var takes priority over ageKeyEnvVar", func(t *testing.T) {
		t.Setenv("TEST_SOPSUTIL_ENV_VAR_NEW", testKey)
		t.Setenv(
			"TEST_SOPSUTIL_ENV_VAR_OLD",
			"AGE-SECRET-KEY-1OLDVAR00000000000000000000000000000000000000000000000000",
		)
		noKeyFileEnv(t)

		sops := v1alpha1.SOPS{
			AgeKeyEnvVar: "TEST_SOPSUTIL_ENV_VAR_OLD",
			Env:          v1alpha1.SOPSEnv{Var: "TEST_SOPSUTIL_ENV_VAR_NEW"},
		}
		got, err := sopsutil.ResolveAgeKey(sops)
		require.NoError(t, err)
		assert.Equal(t, testKey, got)
	})

	t.Run("falls back to ageKeyEnvVar when env.var empty", func(t *testing.T) {
		t.Setenv("TEST_SOPSUTIL_ENV_VAR_FALLBACK", testKey)
		noKeyFileEnv(t)

		sops := v1alpha1.SOPS{
			AgeKeyEnvVar: "TEST_SOPSUTIL_ENV_VAR_FALLBACK",
		}
		got, err := sopsutil.ResolveAgeKey(sops)
		require.NoError(t, err)
		assert.Equal(t, testKey, got)
	})
}

// ---------------------------------------------------------------------------
// ResolveAgeKey: extract.file override
// ---------------------------------------------------------------------------

func TestResolveAgeKey_ExtractFileOverride(t *testing.T) {
	const testKey = "AGE-SECRET-KEY-1CUSTOM00000000000000000000000000000000000000000000000000"

	t.Run("extract.file specifies custom key file", func(t *testing.T) {
		dir := t.TempDir()
		keyPath := filepath.Join(dir, "custom-keys.txt")
		err := os.WriteFile(keyPath, []byte("# custom\n"+testKey+"\n"), 0o600)
		require.NoError(t, err)

		// Set env var to empty to skip env var lookup
		t.Setenv("TEST_SOPSUTIL_EXTRACT_EMPTY", "")

		sops := v1alpha1.SOPS{
			AgeKeyEnvVar: "TEST_SOPSUTIL_EXTRACT_EMPTY",
			Extract:      v1alpha1.SOPSExtract{File: keyPath},
		}
		got, err := sopsutil.ResolveAgeKey(sops)
		require.NoError(t, err)
		assert.Equal(t, testKey, got)
	})
}

// ---------------------------------------------------------------------------
// ResolveAgeKey: multi-key file returns all keys
// ---------------------------------------------------------------------------

func TestResolveAgeKey_MultiKeyFileReturnsAll(t *testing.T) {
	const (
		key1 = "AGE-SECRET-KEY-FIRST000000000000000000000000000000000000000000000000"
		key2 = "AGE-SECRET-KEY-SECOND00000000000000000000000000000000000000000000000"
	)

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "keys.txt")
	err := os.WriteFile(keyPath, []byte(
		"# key 1\n"+key1+"\n# key 2\n"+key2+"\n",
	), 0o600)
	require.NoError(t, err)

	t.Setenv("TEST_SOPSUTIL_MULTI_EMPTY", "")

	sops := v1alpha1.SOPS{
		AgeKeyEnvVar: "TEST_SOPSUTIL_MULTI_EMPTY",
		Extract:      v1alpha1.SOPSExtract{File: keyPath},
	}
	got, err := sopsutil.ResolveAgeKey(sops)
	require.NoError(t, err)
	assert.Equal(t, key1+"\n"+key2, got)
}
