package k8s_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
)

func TestSecretDataContains_Matches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		existing    map[string][]byte
		desiredData map[string][]byte
		expected    bool
	}{
		{
			name:        "all desired keys match",
			existing:    map[string][]byte{"a": []byte("1"), "b": []byte("2")},
			desiredData: map[string][]byte{"a": []byte("1"), "b": []byte("2")},
			expected:    true,
		},
		{
			// existing has extra keys — desired keys still match
			name: "existing has extra keys desired keys still match",
			existing: map[string][]byte{
				"a": []byte("1"), "b": []byte("2"), "extra": []byte("x"),
			},
			desiredData: map[string][]byte{"a": []byte("1"), "b": []byte("2")},
			expected:    true,
		},
		{
			name:        "empty desiredData always matches",
			existing:    map[string][]byte{"a": []byte("1")},
			desiredData: map[string][]byte{},
			expected:    true,
		},
		{
			name:        "nil existing with empty desired returns true",
			existing:    nil,
			desiredData: map[string][]byte{},
			expected:    true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := k8s.SecretDataContains(testCase.existing, testCase.desiredData)
			assert.Equal(t, testCase.expected, got)
		})
	}
}

func TestSecretDataContains_NoMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		existing    map[string][]byte
		desiredData map[string][]byte
	}{
		{
			name:        "one value differs",
			existing:    map[string][]byte{"a": []byte("1"), "b": []byte("changed")},
			desiredData: map[string][]byte{"a": []byte("1"), "b": []byte("2")},
		},
		{
			name:        "desired key missing from existing",
			existing:    map[string][]byte{"a": []byte("1")},
			desiredData: map[string][]byte{"a": []byte("1"), "b": []byte("2")},
		},
		{
			name:        "nil existing with non-empty desired returns false",
			existing:    nil,
			desiredData: map[string][]byte{"a": []byte("1")},
		},
		{
			// A nil desired value must not match a missing key (before the fix,
			// existing[k] also returns nil for missing keys, causing a false positive).
			name:        "nil desired value does not match missing key",
			existing:    map[string][]byte{},
			desiredData: map[string][]byte{"missing": nil},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := k8s.SecretDataContains(testCase.existing, testCase.desiredData)
			assert.False(t, got)
		})
	}
}

func TestMergeSecretData_UpdatesAndPreserves(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		secretData    map[string][]byte
		desiredData   map[string][]byte
		expectChanged bool
		expectData    map[string][]byte
	}{
		{
			name:          "updates secret when values differ",
			secretData:    map[string][]byte{"a": []byte("old")},
			desiredData:   map[string][]byte{"a": []byte("new")},
			expectChanged: true,
			expectData:    map[string][]byte{"a": []byte("new")},
		},
		{
			name:          "no-op when values already match",
			secretData:    map[string][]byte{"a": []byte("same")},
			desiredData:   map[string][]byte{"a": []byte("same")},
			expectChanged: false,
			expectData:    map[string][]byte{"a": []byte("same")},
		},
		{
			name:          "preserves extra keys on update",
			secretData:    map[string][]byte{"a": []byte("old"), "extra": []byte("keep")},
			desiredData:   map[string][]byte{"a": []byte("new")},
			expectChanged: true,
			expectData:    map[string][]byte{"a": []byte("new"), "extra": []byte("keep")},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			secret := &corev1.Secret{Data: testCase.secretData}

			changed := k8s.MergeSecretData(secret, testCase.desiredData)

			assert.Equal(t, testCase.expectChanged, changed)
			assert.Equal(t, testCase.expectData, secret.Data)
		})
	}
}

func TestMergeSecretData_NilHandling(t *testing.T) {
	t.Parallel()

	t.Run("initialises nil Data before writing", func(t *testing.T) {
		t.Parallel()

		secret := &corev1.Secret{}
		changed := k8s.MergeSecretData(secret, map[string][]byte{"a": []byte("val")})

		assert.True(t, changed)
		assert.Equal(t, map[string][]byte{"a": []byte("val")}, secret.Data)
	})

	t.Run("sets StringData to nil on change", func(t *testing.T) {
		t.Parallel()

		secret := &corev1.Secret{
			Data:       map[string][]byte{"a": []byte("old")},
			StringData: map[string]string{"shouldBeCleared": "yes"},
		}

		changed := k8s.MergeSecretData(secret, map[string][]byte{"a": []byte("new")})

		assert.True(t, changed)
		assert.Nil(t, secret.StringData)
	})
}
