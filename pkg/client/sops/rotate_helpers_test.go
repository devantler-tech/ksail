package sops_test

import (
	"testing"

	"filippo.io/age"
	sopsclient "github.com/devantler-tech/ksail/v7/pkg/client/sops"
	"github.com/getsops/sops/v3"
	sopsage "github.com/getsops/sops/v3/age"
	"github.com/getsops/sops/v3/keys"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsSupportedExtension(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "yaml", path: "file.yaml", want: true},
		{name: "yml", path: "file.yml", want: true},
		{name: "json", path: "file.json", want: true},
		{name: "toml", path: "file.toml", want: false},
		{name: "txt", path: "file.txt", want: false},
		{name: "no extension", path: "file", want: false},
		{name: "hidden yaml", path: ".secret.yaml", want: true},
		{name: "nested path yaml", path: "/path/to/config.yaml", want: true},
		{name: "nested path json", path: "/path/to/data.json", want: true},
		{name: "xml", path: "config.xml", want: false},
		{name: "env", path: ".env", want: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := sopsclient.IsSupportedExtension(testCase.path)

			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestIsHiddenDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		dir  string
		want bool
	}{
		{name: "dot-prefixed directory", dir: ".git", want: true},
		{name: "dot-config directory", dir: ".config", want: true},
		{name: "normal directory", dir: "config", want: false},
		{name: "underscore directory", dir: "_internal", want: false},
		{name: "current directory dot", dir: ".", want: false},
		{name: "double dot", dir: "..", want: false},
		{name: "hidden with number", dir: ".ssh2", want: true},
		{name: "empty string", dir: "", want: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := sopsclient.IsHiddenDir(testCase.dir)

			assert.Equal(t, testCase.want, got)
		})
	}
}

//nolint:funlen // Table-driven test coverage is naturally long.
func TestModifyKeyGroups(t *testing.T) {
	t.Parallel()

	createAgeKey := func(t *testing.T) sopsage.MasterKey {
		t.Helper()

		identity, err := age.GenerateX25519Identity()
		require.NoError(t, err)

		key, err := sopsage.MasterKeyFromRecipient(identity.Recipient().String())
		require.NoError(t, err)

		return *key
	}

	t.Run("add keys to empty groups", func(t *testing.T) {
		t.Parallel()

		key := createAgeKey(t)
		metadata := &sops.Metadata{
			KeyGroups: []sops.KeyGroup{},
		}

		opts := sopsclient.RotateOpts{
			AddKeys: []keys.MasterKey{&key},
		}

		sopsclient.ModifyKeyGroups(metadata, opts)

		require.Len(t, metadata.KeyGroups, 1)
		require.Len(t, metadata.KeyGroups[0], 1)
	})

	t.Run("add keys to existing group", func(t *testing.T) {
		t.Parallel()

		existingKey := createAgeKey(t)
		newKey := createAgeKey(t)

		metadata := &sops.Metadata{
			KeyGroups: []sops.KeyGroup{{&existingKey}},
		}

		opts := sopsclient.RotateOpts{
			AddKeys: []keys.MasterKey{&newKey},
		}

		sopsclient.ModifyKeyGroups(metadata, opts)

		require.Len(t, metadata.KeyGroups, 1)
		require.Len(t, metadata.KeyGroups[0], 2)
	})

	t.Run("remove key from groups", func(t *testing.T) {
		t.Parallel()

		key1 := createAgeKey(t)
		key2 := createAgeKey(t)

		metadata := &sops.Metadata{
			KeyGroups: []sops.KeyGroup{{&key1, &key2}},
		}

		opts := sopsclient.RotateOpts{
			RemoveKeys: []string{key2.ToString()},
		}

		sopsclient.ModifyKeyGroups(metadata, opts)

		require.Len(t, metadata.KeyGroups, 1)
		require.Len(t, metadata.KeyGroups[0], 1)
		assert.Equal(t, key1.ToString(), metadata.KeyGroups[0][0].ToString())
	})

	t.Run("removing all keys from group removes the group", func(t *testing.T) {
		t.Parallel()

		key := createAgeKey(t)

		metadata := &sops.Metadata{
			KeyGroups: []sops.KeyGroup{{&key}},
		}

		opts := sopsclient.RotateOpts{
			RemoveKeys: []string{key.ToString()},
		}

		sopsclient.ModifyKeyGroups(metadata, opts)

		assert.Empty(t, metadata.KeyGroups)
	})

	t.Run("no-op when no adds or removes", func(t *testing.T) {
		t.Parallel()

		key := createAgeKey(t)
		metadata := &sops.Metadata{
			KeyGroups: []sops.KeyGroup{{&key}},
		}

		opts := sopsclient.RotateOpts{}

		sopsclient.ModifyKeyGroups(metadata, opts)

		require.Len(t, metadata.KeyGroups, 1)
		require.Len(t, metadata.KeyGroups[0], 1)
	})
}

//nolint:funlen // Table-driven test coverage is naturally long.
func TestRemoveKeyFromGroups(t *testing.T) {
	t.Parallel()

	createAgeKey := func(t *testing.T) sopsage.MasterKey {
		t.Helper()

		identity, err := age.GenerateX25519Identity()
		require.NoError(t, err)

		key, err := sopsage.MasterKeyFromRecipient(identity.Recipient().String())
		require.NoError(t, err)

		return *key
	}

	t.Run("remove existing key from single group", func(t *testing.T) {
		t.Parallel()

		key1 := createAgeKey(t)
		key2 := createAgeKey(t)

		groups := []sops.KeyGroup{{&key1, &key2}}

		result := sopsclient.RemoveKeyFromGroups(groups, key1.ToString())

		require.Len(t, result, 1)
		require.Len(t, result[0], 1)
		assert.Equal(t, key2.ToString(), result[0][0].ToString())
	})

	t.Run("remove key from multiple groups", func(t *testing.T) {
		t.Parallel()

		key1 := createAgeKey(t)
		key2 := createAgeKey(t)
		key3 := createAgeKey(t)

		groups := []sops.KeyGroup{
			{&key1, &key2},
			{&key2, &key3},
		}

		result := sopsclient.RemoveKeyFromGroups(groups, key2.ToString())

		require.Len(t, result, 2)
		require.Len(t, result[0], 1)
		require.Len(t, result[1], 1)
		assert.Equal(t, key1.ToString(), result[0][0].ToString())
		assert.Equal(t, key3.ToString(), result[1][0].ToString())
	})

	t.Run("remove nonexistent key is no-op", func(t *testing.T) {
		t.Parallel()

		key := createAgeKey(t)

		groups := []sops.KeyGroup{{&key}}

		result := sopsclient.RemoveKeyFromGroups(groups, "nonexistent-key")

		require.Len(t, result, 1)
		require.Len(t, result[0], 1)
	})

	t.Run("empty groups returns empty", func(t *testing.T) {
		t.Parallel()

		result := sopsclient.RemoveKeyFromGroups(nil, "any-key")

		assert.Empty(t, result)
	})
}
