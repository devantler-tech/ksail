package registry_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/stretchr/testify/assert"
)

func TestMergeSpecs(t *testing.T) { //nolint:funlen // table-driven test with many cases
	t.Parallel()

	testCases := []struct {
		name          string
		existingSpecs []registry.MirrorSpec
		flagSpecs     []registry.MirrorSpec
		expected      []registry.MirrorSpec
	}{
		{
			name:          "emptyInputs",
			existingSpecs: nil,
			flagSpecs:     nil,
			expected:      []registry.MirrorSpec{},
		},
		{
			name:          "nilExistingEmptyFlag",
			existingSpecs: nil,
			flagSpecs:     []registry.MirrorSpec{},
			expected:      []registry.MirrorSpec{},
		},
		{
			name:          "emptyExistingNilFlag",
			existingSpecs: []registry.MirrorSpec{},
			flagSpecs:     nil,
			expected:      []registry.MirrorSpec{},
		},
		{
			name: "onlyExistingSpecs",
			existingSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
				{Host: "ghcr.io", Remote: "https://ghcr.io"},
			},
			flagSpecs: nil,
			expected: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
				{Host: "ghcr.io", Remote: "https://ghcr.io"},
			},
		},
		{
			name:          "onlyFlagSpecs",
			existingSpecs: nil,
			flagSpecs: []registry.MirrorSpec{
				{Host: "quay.io", Remote: "https://quay.io"},
				{Host: "gcr.io", Remote: "https://gcr.io"},
			},
			expected: []registry.MirrorSpec{
				{Host: "gcr.io", Remote: "https://gcr.io"},
				{Host: "quay.io", Remote: "https://quay.io"},
			},
		},
		{
			name: "noOverlap",
			existingSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
			},
			flagSpecs: []registry.MirrorSpec{
				{Host: "ghcr.io", Remote: "https://ghcr.io"},
			},
			expected: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
				{Host: "ghcr.io", Remote: "https://ghcr.io"},
			},
		},
		{
			name: "flagOverridesExisting",
			existingSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://old-registry.docker.io"},
				{Host: "ghcr.io", Remote: "https://ghcr.io"},
			},
			flagSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://new-registry.docker.io"},
			},
			expected: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://new-registry.docker.io"},
				{Host: "ghcr.io", Remote: "https://ghcr.io"},
			},
		},
		{
			name: "multipleFlagOverrides",
			existingSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://old-docker.io"},
				{Host: "ghcr.io", Remote: "https://old-ghcr.io"},
				{Host: "quay.io", Remote: "https://quay.io"},
			},
			flagSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://new-docker.io"},
				{Host: "ghcr.io", Remote: "https://new-ghcr.io"},
			},
			expected: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://new-docker.io"},
				{Host: "ghcr.io", Remote: "https://new-ghcr.io"},
				{Host: "quay.io", Remote: "https://quay.io"},
			},
		},
		{
			name: "duplicateHostsInSameInput",
			existingSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://first.docker.io"},
				{Host: "docker.io", Remote: "https://second.docker.io"},
			},
			flagSpecs: nil,
			expected: []registry.MirrorSpec{
				// Last one wins when building the map
				{Host: "docker.io", Remote: "https://second.docker.io"},
			},
		},
		{
			name: "deterministicOrderWithMultipleHosts",
			existingSpecs: []registry.MirrorSpec{
				{Host: "zzz.io", Remote: "https://zzz.io"},
				{Host: "aaa.io", Remote: "https://aaa.io"},
				{Host: "mmm.io", Remote: "https://mmm.io"},
			},
			flagSpecs: []registry.MirrorSpec{
				{Host: "bbb.io", Remote: "https://bbb.io"},
			},
			expected: []registry.MirrorSpec{
				{Host: "aaa.io", Remote: "https://aaa.io"},
				{Host: "bbb.io", Remote: "https://bbb.io"},
				{Host: "mmm.io", Remote: "https://mmm.io"},
				{Host: "zzz.io", Remote: "https://zzz.io"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := registry.MergeSpecs(tc.existingSpecs, tc.flagSpecs)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestMergeSpecs_DeterministicOrder(t *testing.T) {
	t.Parallel()

	existingSpecs := []registry.MirrorSpec{
		{Host: "docker.io", Remote: "https://registry-1.docker.io"},
		{Host: "ghcr.io", Remote: "https://ghcr.io"},
		{Host: "quay.io", Remote: "https://quay.io"},
	}

	flagSpecs := []registry.MirrorSpec{
		{Host: "gcr.io", Remote: "https://gcr.io"},
	}

	// Run multiple times to ensure deterministic output
	var previousResult []registry.MirrorSpec

	for i := range 10 {
		result := registry.MergeSpecs(existingSpecs, flagSpecs)

		if i > 0 {
			assert.Equal(
				t,
				previousResult,
				result,
				"Results should be identical across multiple runs",
			)
		}

		previousResult = result
	}

	// Verify the result is sorted
	expected := []registry.MirrorSpec{
		{Host: "docker.io", Remote: "https://registry-1.docker.io"},
		{Host: "gcr.io", Remote: "https://gcr.io"},
		{Host: "ghcr.io", Remote: "https://ghcr.io"},
		{Host: "quay.io", Remote: "https://quay.io"},
	}
	assert.Equal(t, expected, previousResult)
}
