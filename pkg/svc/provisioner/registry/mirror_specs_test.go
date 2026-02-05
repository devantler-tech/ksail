package registry_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestAllocatePort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		initPort     int
		usedPorts    map[int]struct{}
		expectedPort int
		expectedNext int
	}{
		{
			name:         "first allocation from default",
			initPort:     5000,
			usedPorts:    map[int]struct{}{},
			expectedPort: 5000,
			expectedNext: 5001,
		},
		{
			name:         "skips used port",
			initPort:     5000,
			usedPorts:    map[int]struct{}{5000: {}},
			expectedPort: 5001,
			expectedNext: 5002,
		},
		{
			name:         "skips multiple used ports",
			initPort:     5000,
			usedPorts:    map[int]struct{}{5000: {}, 5001: {}, 5002: {}},
			expectedPort: 5003,
			expectedNext: 5004,
		},
		{
			name:         "nil usedPorts creates new map",
			initPort:     5000,
			usedPorts:    nil,
			expectedPort: 5000,
			expectedNext: 5001,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			nextPort := tc.initPort
			usedPorts := tc.usedPorts

			port := registry.AllocatePort(&nextPort, usedPorts)

			assert.Equal(t, tc.expectedPort, port)
			assert.Equal(t, tc.expectedNext, nextPort)
		})
	}
}

func TestAllocatePort_NilNextPort(t *testing.T) {
	t.Parallel()

	port := registry.AllocatePort(nil, nil)

	assert.Equal(t, 5000, port)
}

func TestAllocatePort_Sequential(t *testing.T) {
	t.Parallel()

	nextPort := 5000
	usedPorts := map[int]struct{}{}

	port1 := registry.AllocatePort(&nextPort, usedPorts)
	port2 := registry.AllocatePort(&nextPort, usedPorts)
	port3 := registry.AllocatePort(&nextPort, usedPorts)

	assert.Equal(t, 5000, port1)
	assert.Equal(t, 5001, port2)
	assert.Equal(t, 5002, port3)
}

func TestInitPortAllocation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		baseUsed     map[int]struct{}
		expectedNext int
		expectedLen  int
	}{
		{
			name:         "empty base",
			baseUsed:     map[int]struct{}{},
			expectedNext: 5000,
			expectedLen:  0,
		},
		{
			name:         "single port",
			baseUsed:     map[int]struct{}{5000: {}},
			expectedNext: 5001,
			expectedLen:  1,
		},
		{
			name:         "multiple ports with gap",
			baseUsed:     map[int]struct{}{5000: {}, 5002: {}},
			expectedNext: 5003,
			expectedLen:  2,
		},
		{
			name:         "port below default",
			baseUsed:     map[int]struct{}{3000: {}},
			expectedNext: 5000,
			expectedLen:  1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			usedPorts, nextPort := registry.InitPortAllocation(tc.baseUsed)

			assert.Equal(t, tc.expectedNext, nextPort)
			assert.Len(t, usedPorts, tc.expectedLen)
		})
	}
}

//nolint:funlen // Table-driven test with comprehensive test cases.
func TestBuildUpstreamLookup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		specs    []registry.MirrorSpec
		expected map[string]string
	}{
		{
			name:     "nil specs",
			specs:    nil,
			expected: nil,
		},
		{
			name:     "empty specs",
			specs:    []registry.MirrorSpec{},
			expected: nil,
		},
		{
			name: "single spec",
			specs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
			},
			expected: map[string]string{
				"docker.io": "https://registry-1.docker.io",
			},
		},
		{
			name: "empty host skipped",
			specs: []registry.MirrorSpec{
				{Host: "", Remote: "https://example.com"},
			},
			expected: nil,
		},
		{
			name: "empty remote skipped",
			specs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: ""},
			},
			expected: nil,
		},
		{
			name: "multiple specs",
			specs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
				{Host: "ghcr.io", Remote: "https://ghcr.io"},
			},
			expected: map[string]string{
				"docker.io": "https://registry-1.docker.io",
				"ghcr.io":   "https://ghcr.io",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := registry.BuildUpstreamLookup(tc.specs)

			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestRenderK3dMirrorConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    map[string][]string
		expected string
	}{
		{
			name:     "empty map",
			input:    map[string][]string{},
			expected: "",
		},
		{
			name: "single host single endpoint",
			input: map[string][]string{
				"docker.io": {"https://registry-1.docker.io"},
			},
			expected: "mirrors:\n  \"docker.io\":\n    endpoint:\n      - https://registry-1.docker.io\n",
		},
		{
			name: "single host multiple endpoints",
			input: map[string][]string{
				"docker.io": {"http://mirror:5000", "https://registry-1.docker.io"},
			},
			expected: "mirrors:\n  \"docker.io\":\n    endpoint:\n      - http://mirror:5000\n      - https://registry-1.docker.io\n",
		},
		{
			name: "multiple hosts sorted",
			input: map[string][]string{
				"ghcr.io":   {"https://ghcr.io"},
				"docker.io": {"https://registry-1.docker.io"},
			},
			expected: "mirrors:\n  \"docker.io\":\n    endpoint:\n      - https://registry-1.docker.io\n  \"ghcr.io\":\n    endpoint:\n      - https://ghcr.io\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := registry.RenderK3dMirrorConfig(tc.input)

			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestGenerateScaffoldedHostsToml(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		spec     registry.MirrorSpec
		contains []string
	}{
		{
			name: "docker.io with remote",
			spec: registry.MirrorSpec{
				Host:   "docker.io",
				Remote: "https://registry-1.docker.io",
			},
			contains: []string{
				`server = "https://registry-1.docker.io"`,
				`[host."http://docker.io:5000"]`,
				`capabilities = ["pull", "resolve"]`,
			},
		},
		{
			name: "host without remote uses generated URL",
			spec: registry.MirrorSpec{
				Host: "ghcr.io",
			},
			contains: []string{
				`server = "https://ghcr.io"`,
				`[host."http://ghcr.io:5000"]`,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := registry.GenerateScaffoldedHostsToml(tc.spec)

			for _, expected := range tc.contains {
				assert.Contains(t, result, expected)
			}
		})
	}
}

//nolint:funlen // Table-driven test with comprehensive test cases.
func TestBuildMirrorEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		specs         []registry.MirrorSpec
		prefix        string
		existingHosts map[string]struct{}
		expectedLen   int
	}{
		{
			name:        "empty specs",
			specs:       []registry.MirrorSpec{},
			prefix:      "kind",
			expectedLen: 0,
		},
		{
			name: "single spec with prefix",
			specs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
			},
			prefix:      "kind",
			expectedLen: 1,
		},
		{
			name: "empty prefix",
			specs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
			},
			prefix:      "",
			expectedLen: 1,
		},
		{
			name: "existing host skipped",
			specs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
			},
			prefix:        "kind",
			existingHosts: map[string]struct{}{"docker.io": {}},
			expectedLen:   0,
		},
		{
			name: "empty host spec skipped",
			specs: []registry.MirrorSpec{
				{Host: "", Remote: "https://example.com"},
			},
			prefix:      "kind",
			expectedLen: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			nextPort := 5000
			usedPorts := map[int]struct{}{}

			entries := registry.BuildMirrorEntries(
				tc.specs, tc.prefix, tc.existingHosts, usedPorts, &nextPort,
			)

			assert.Len(t, entries, tc.expectedLen)
		})
	}
}

func TestBuildMirrorEntries_FieldValues(t *testing.T) {
	t.Parallel()

	specs := []registry.MirrorSpec{
		{Host: "docker.io", Remote: "https://registry-1.docker.io"},
	}
	nextPort := 5000
	usedPorts := map[int]struct{}{}

	entries := registry.BuildMirrorEntries(specs, "kind", nil, usedPorts, &nextPort)

	require.Len(t, entries, 1)

	entry := entries[0]
	assert.Equal(t, "docker.io", entry.Host)
	assert.Equal(t, "https://registry-1.docker.io", entry.Remote)
	assert.Equal(t, 5000, entry.Port)
	assert.Contains(t, entry.ContainerName, "kind-")
	assert.NotEmpty(t, entry.SanitizedName)
	assert.Contains(t, entry.Endpoint, "http://")
}
