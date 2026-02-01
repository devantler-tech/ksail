package envvar_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/utils/envvar"
	"github.com/stretchr/testify/assert"
)

// Note: Tests using t.Setenv cannot be run in parallel, so we run them sequentially.

type expandTestCase struct {
	name     string
	input    string
	envVars  map[string]string
	expected string
}

func getExpandTestCasesWithNoEnvVars() []expandTestCase {
	return []expandTestCase{
		{
			name:     "empty string",
			input:    "",
			envVars:  nil,
			expected: "",
		},
		{
			name:     "no placeholders",
			input:    "plain text without any variables",
			envVars:  nil,
			expected: "plain text without any variables",
		},
		{
			name:     "single placeholder without value",
			input:    "Hello ${UNDEFINED_VAR}!",
			envVars:  nil,
			expected: "Hello !",
		},
		{
			name:     "invalid placeholder syntax - no braces",
			input:    "$NAME",
			envVars:  nil,
			expected: "$NAME",
		},
		{
			name:     "invalid placeholder syntax - single brace",
			input:    "${NAME",
			envVars:  nil,
			expected: "${NAME",
		},
		{
			name:     "empty placeholder",
			input:    "${}",
			envVars:  nil,
			expected: "${}",
		},
	}
}

func getExpandTestCasesWithDefaultValues() []expandTestCase {
	return []expandTestCase{
		{
			name:     "default value - undefined var uses default",
			input:    "${UNDEFINED_VAR:-fallback}",
			envVars:  nil,
			expected: "fallback",
		},
		{
			name:     "default value - defined var ignores default",
			input:    "${TEST_DEFINED:-fallback}",
			envVars:  map[string]string{"TEST_DEFINED": "actual"},
			expected: "actual",
		},
		{
			name:     "default value - empty default for undefined",
			input:    "${UNDEFINED_VAR:-}",
			envVars:  nil,
			expected: "",
		},
		{
			name:     "default value - with port number",
			input:    "${REGISTRY:-localhost:5000}",
			envVars:  nil,
			expected: "localhost:5000",
		},
		{
			name:     "default value - with path",
			input:    "${CONFIG_PATH:-/etc/config/default.yaml}",
			envVars:  nil,
			expected: "/etc/config/default.yaml",
		},
		{
			name:     "default value - URL",
			input:    "endpoint: ${ENDPOINT:-http://localhost:8080/api}",
			envVars:  nil,
			expected: "endpoint: http://localhost:8080/api",
		},
		{
			name:     "default value - multiple with defaults",
			input:    "${HOST:-localhost}:${PORT:-8080}",
			envVars:  nil,
			expected: "localhost:8080",
		},
		{
			name:     "default value - mixed defined and default",
			input:    "${HOST:-localhost}:${PORT:-8080}",
			envVars:  map[string]string{"HOST": "example.com"},
			expected: "example.com:8080",
		},
		{
			name:     "default value - empty string env var overrides default",
			input:    "${EMPTY_VAR:-fallback}",
			envVars:  map[string]string{"EMPTY_VAR": ""},
			expected: "",
		},
	}
}

func getExpandTestCasesWithEnvVarsBasic() []expandTestCase {
	return []expandTestCase{
		{
			name:     "single placeholder with value",
			input:    "Hello ${TEST_NAME}!",
			envVars:  map[string]string{"TEST_NAME": "World"},
			expected: "Hello World!",
		},
		{
			name:  "multiple placeholders",
			input: "${TEST_GREETING} ${TEST_TARGET}, welcome to ${TEST_PLACE}",
			envVars: map[string]string{
				"TEST_GREETING": "Hello",
				"TEST_TARGET":   "User",
				"TEST_PLACE":    "Home",
			},
			expected: "Hello User, welcome to Home",
		},
		{
			name:     "mixed defined and undefined",
			input:    "${TEST_DEFINED} and ${TEST_UNDEFINED_XYZ}",
			envVars:  map[string]string{"TEST_DEFINED": "value"},
			expected: "value and ",
		},
		{
			name:     "variable with underscore",
			input:    "${TEST_MY_VAR_NAME}",
			envVars:  map[string]string{"TEST_MY_VAR_NAME": "test"},
			expected: "test",
		},
		{
			name:     "variable with numbers",
			input:    "${TEST_VAR123}",
			envVars:  map[string]string{"TEST_VAR123": "numeric"},
			expected: "numeric",
		},
	}
}

func getExpandTestCasesWithEnvVarsAdvanced() []expandTestCase {
	return []expandTestCase{
		{
			name:     "variable starting with underscore",
			input:    "${_TEST_PRIVATE}",
			envVars:  map[string]string{"_TEST_PRIVATE": "secret"},
			expected: "secret",
		},
		{
			name:     "adjacent placeholders",
			input:    "${TEST_A}${TEST_B}${TEST_C}",
			envVars:  map[string]string{"TEST_A": "1", "TEST_B": "2", "TEST_C": "3"},
			expected: "123",
		},
		{
			name:     "placeholder in path",
			input:    "/home/${TEST_USER}/config/${TEST_APP_NAME}.yaml",
			envVars:  map[string]string{"TEST_USER": "developer", "TEST_APP_NAME": "ksail"},
			expected: "/home/developer/config/ksail.yaml",
		},
		{
			name:     "URL with placeholder",
			input:    "https://${TEST_HOST}:${TEST_PORT}/api",
			envVars:  map[string]string{"TEST_HOST": "localhost", "TEST_PORT": "8080"},
			expected: "https://localhost:8080/api",
		},
		{
			name:     "placeholder with special chars not matching regex",
			input:    "${VAR-NAME}",
			envVars:  map[string]string{"VAR-NAME": "value"},
			expected: "${VAR-NAME}",
		},
		{
			name:     "nested braces - inner variable expanded",
			input:    "${${TEST_INNER}}",
			envVars:  map[string]string{"TEST_INNER": "nested"},
			expected: "${nested}",
		},
	}
}

func getExpandTestCasesWithEnvVars() []expandTestCase {
	return append(getExpandTestCasesWithEnvVarsBasic(), getExpandTestCasesWithEnvVarsAdvanced()...)
}

func TestExpand_NoEnvVars(t *testing.T) {
	t.Parallel()

	tests := getExpandTestCasesWithNoEnvVars()

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := envvar.Expand(testCase.input)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestExpand_WithEnvVars(t *testing.T) {
	// Note: Cannot use t.Parallel() when using t.Setenv()
	tests := getExpandTestCasesWithEnvVars()

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			// Set environment variables for this test
			for key, value := range testCase.envVars {
				t.Setenv(key, value)
			}

			result := envvar.Expand(testCase.input)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestExpand_HomePath(t *testing.T) {
	t.Setenv("TEST_HOME", "/test/home")

	result := envvar.Expand("${TEST_HOME}/config")
	assert.Equal(t, "/test/home/config", result)
}

func TestExpand_PathLikeVariable(t *testing.T) {
	t.Setenv("TEST_PATH", "/usr/bin:/usr/local/bin")

	result := envvar.Expand("Paths: ${TEST_PATH}")
	assert.Equal(t, "Paths: /usr/bin:/usr/local/bin", result)
}

func TestExpand_DefaultValues(t *testing.T) {
	// Note: Cannot use t.Parallel() when using t.Setenv()
	tests := getExpandTestCasesWithDefaultValues()

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			// Set environment variables for this test
			for key, value := range testCase.envVars {
				t.Setenv(key, value)
			}

			result := envvar.Expand(testCase.input)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestExpandBytes(t *testing.T) {
	t.Setenv("TEST_VAR", "expanded")

	input := []byte("value: ${TEST_VAR}")
	expected := []byte("value: expanded")

	result := envvar.ExpandBytes(input)
	assert.Equal(t, expected, result)
}

func TestExpandBytes_WithDefaultValue(t *testing.T) { //nolint:paralleltest // Uses t.Setenv
	input := []byte("registry: ${REGISTRY:-localhost:5000}")
	expected := []byte("registry: localhost:5000")

	result := envvar.ExpandBytes(input)
	assert.Equal(t, expected, result)
}

func TestExpandBytes_YAMLContent(t *testing.T) {
	t.Setenv("TEST_REGISTRY", "myregistry.io:5000")

	input := []byte(`apiVersion: kind.x-k8s.io/v1alpha4
kind: Cluster
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."${TEST_REGISTRY}"]
    endpoint = ["http://${TEST_REGISTRY}"]`)

	expected := []byte(`apiVersion: kind.x-k8s.io/v1alpha4
kind: Cluster
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."myregistry.io:5000"]
    endpoint = ["http://myregistry.io:5000"]`)

	result := envvar.ExpandBytes(input)
	assert.Equal(t, expected, result)
}
