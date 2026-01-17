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

func getExpandTestCasesWithEnvVars() []expandTestCase {
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
