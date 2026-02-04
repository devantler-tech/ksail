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

func TestMirrorSpec_HasCredentials(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		spec     registry.MirrorSpec
		expected bool
	}{
		{
			name: "no_credentials",
			spec: registry.MirrorSpec{
				Host:   "docker.io",
				Remote: "https://registry-1.docker.io",
			},
			expected: false,
		},
		{
			name: "username_only",
			spec: registry.MirrorSpec{
				Host:     "ghcr.io",
				Remote:   "https://ghcr.io",
				Username: "myuser",
			},
			expected: true,
		},
		{
			name: "password_only",
			spec: registry.MirrorSpec{
				Host:     "quay.io",
				Remote:   "https://quay.io",
				Password: "mypass",
			},
			expected: true,
		},
		{
			name: "both_credentials",
			spec: registry.MirrorSpec{
				Host:     "gcr.io",
				Remote:   "https://gcr.io",
				Username: "user",
				Password: "pass",
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := tc.spec.HasCredentials()
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestMirrorSpec_ResolveCredentials_Basic(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		spec             registry.MirrorSpec
		expectedUsername string
		expectedPassword string
	}{
		{
			name: "no_credentials",
			spec: registry.MirrorSpec{
				Host:   "docker.io",
				Remote: "https://registry-1.docker.io",
			},
			expectedUsername: "",
			expectedPassword: "",
		},
		{
			name: "literal_credentials",
			spec: registry.MirrorSpec{
				Host:     "ghcr.io",
				Remote:   "https://ghcr.io",
				Username: "myuser",
				Password: "mypass",
			},
			expectedUsername: "myuser",
			expectedPassword: "mypass",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			username, password := testCase.spec.ResolveCredentials()
			assert.Equal(t, testCase.expectedUsername, username)
			assert.Equal(t, testCase.expectedPassword, password)
		})
	}
}

func TestMirrorSpec_ResolveCredentials_EnvVars(t *testing.T) {
	// Set test environment variables
	// Note: Cannot use t.Parallel() with t.Setenv() as it would create race conditions
	t.Setenv("TEST_USER", "github-user")
	t.Setenv("TEST_TOKEN", "ghp_test1234")

	testCases := []struct {
		name             string
		spec             registry.MirrorSpec
		expectedUsername string
		expectedPassword string
	}{
		{
			name: "env_var_credentials",
			spec: registry.MirrorSpec{
				Host:     "ghcr.io",
				Remote:   "https://ghcr.io",
				Username: "${TEST_USER}",
				Password: "${TEST_TOKEN}",
			},
			expectedUsername: "github-user",
			expectedPassword: "ghp_test1234",
		},
		{
			name: "mixed_credentials",
			spec: registry.MirrorSpec{
				Host:     "quay.io",
				Remote:   "https://quay.io",
				Username: "literal-user",
				Password: "${TEST_TOKEN}",
			},
			expectedUsername: "literal-user",
			expectedPassword: "ghp_test1234",
		},
		{
			name: "undefined_env_var",
			spec: registry.MirrorSpec{
				Host:     "gcr.io",
				Remote:   "https://gcr.io",
				Username: "${UNDEFINED_VAR}",
				Password: "pass",
			},
			expectedUsername: "",
			expectedPassword: "pass",
		},
	}

	for _, testCase := range testCases { //nolint:paralleltest // t.Setenv mutates global env
		t.Run(testCase.name, func(t *testing.T) {
			// Note: Cannot use t.Parallel() here - t.Setenv() in parent test
			// mutates global process environment, causing race conditions
			username, password := testCase.spec.ResolveCredentials()
			assert.Equal(t, testCase.expectedUsername, username)
			assert.Equal(t, testCase.expectedPassword, password)
		})
	}
}

func TestParseMirrorSpecs_NoCredentials(t *testing.T) {
	t.Parallel()

	specs := []string{
		"docker.io=https://registry-1.docker.io",
	}
	expected := []registry.MirrorSpec{
		{
			Host:     "docker.io",
			Remote:   "https://registry-1.docker.io",
			Username: "",
			Password: "",
		},
	}

	result := registry.ParseMirrorSpecs(specs)
	assert.Equal(t, expected, result)
}

func TestParseMirrorSpecs_UsernamePassword(t *testing.T) {
	t.Parallel()

	specs := []string{"user:pass@ghcr.io=https://ghcr.io"}
	expected := []registry.MirrorSpec{{
		Host:     "ghcr.io",
		Remote:   "https://ghcr.io",
		Username: "user",
		Password: "pass",
	}}

	result := registry.ParseMirrorSpecs(specs)
	assert.Equal(t, expected, result)
}

func TestParseMirrorSpecs_UsernameOnly(t *testing.T) {
	t.Parallel()

	specs := []string{"user@ghcr.io=https://ghcr.io"}
	expected := []registry.MirrorSpec{{
		Host:     "ghcr.io",
		Remote:   "https://ghcr.io",
		Username: "user",
		Password: "",
	}}

	result := registry.ParseMirrorSpecs(specs)
	assert.Equal(t, expected, result)
}

func TestParseMirrorSpecs_EnvVarCredentials(t *testing.T) {
	t.Parallel()

	specs := []string{
		"${USER}:${TOKEN}@docker.io=https://registry-1.docker.io",
	}
	expected := []registry.MirrorSpec{
		{
			Host:     "docker.io",
			Remote:   "https://registry-1.docker.io",
			Username: "${USER}",
			Password: "${TOKEN}",
		},
	}

	result := registry.ParseMirrorSpecs(specs)
	assert.Equal(t, expected, result)
}

func TestParseMirrorSpecs_MixedSpecs(t *testing.T) {
	t.Parallel()

	specs := []string{
		"user:pass@ghcr.io=https://ghcr.io",
		"docker.io=https://registry-1.docker.io",
	}
	expected := []registry.MirrorSpec{
		{
			Host:     "ghcr.io",
			Remote:   "https://ghcr.io",
			Username: "user",
			Password: "pass",
		},
		{
			Host:     "docker.io",
			Remote:   "https://registry-1.docker.io",
			Username: "",
			Password: "",
		},
	}

	result := registry.ParseMirrorSpecs(specs)
	assert.Equal(t, expected, result)
}

func TestParseMirrorSpecs_AtSignInURL(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		specs    []string
		expected []registry.MirrorSpec
	}{
		{
			name: "at_sign_in_remote_url_not_parsed_as_credentials",
			specs: []string{
				"docker.io=https://user:pass@registry-1.docker.io",
			},
			expected: []registry.MirrorSpec{
				{
					Host:     "docker.io",
					Remote:   "https://user:pass@registry-1.docker.io",
					Username: "",
					Password: "",
				},
			},
		},
		{
			name: "credentials_with_at_sign_in_remote_url",
			specs: []string{
				"myuser:mypass@docker.io=https://user:pass@registry-1.docker.io",
			},
			expected: []registry.MirrorSpec{
				{
					Host:     "docker.io",
					Remote:   "https://user:pass@registry-1.docker.io",
					Username: "myuser",
					Password: "mypass",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := registry.ParseMirrorSpecs(tc.specs)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestBuildRegistryInfosFromSpecs_WithCredentials(t *testing.T) {
	t.Parallel()

	specs := []registry.MirrorSpec{
		{
			Host:     "ghcr.io",
			Remote:   "https://ghcr.io",
			Username: "${GITHUB_USER}",
			Password: "${GITHUB_TOKEN}",
		},
		{
			Host:   "docker.io",
			Remote: "https://registry-1.docker.io",
		},
	}

	infos := registry.BuildRegistryInfosFromSpecs(specs, nil, nil, "test")

	assert.Len(t, infos, 2)

	// Check first registry with credentials
	assert.Equal(t, "ghcr.io", infos[0].Host)
	assert.Equal(t, "${GITHUB_USER}", infos[0].Username)
	assert.Equal(t, "${GITHUB_TOKEN}", infos[0].Password)

	// Check second registry without credentials
	assert.Equal(t, "docker.io", infos[1].Host)
	assert.Empty(t, infos[1].Username)
	assert.Empty(t, infos[1].Password)
}
