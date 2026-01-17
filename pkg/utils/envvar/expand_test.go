package envvar_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/utils/envvar"
	"github.com/stretchr/testify/assert"
)

type expandTestCase struct {
	name     string
	input    string
	envVars  map[string]string
	expected string
}

func expandTestCases() []expandTestCase {
	return []expandTestCase{
		{name: "empty string", input: "", envVars: nil, expected: ""},
		{name: "no placeholders", input: "hello world", envVars: nil, expected: "hello world"},
		{
			name: "single placeholder with value", input: "hello ${NAME}",
			envVars: map[string]string{"NAME": "world"}, expected: "hello world",
		},
		{
			name: "single placeholder without value", input: "hello ${MISSING}",
			envVars: nil, expected: "hello ",
		},
		{
			name:     "multiple placeholders",
			input:    "${GREETING} ${NAME}!",
			envVars:  map[string]string{"GREETING": "Hello", "NAME": "World"},
			expected: "Hello World!",
		},
		{
			name: "placeholder with underscores", input: "${MY_VAR_NAME}",
			envVars: map[string]string{"MY_VAR_NAME": "value"}, expected: "value",
		},
		{
			name: "placeholder with numbers", input: "${VAR123}",
			envVars: map[string]string{"VAR123": "numeric"}, expected: "numeric",
		},
		{
			name: "invalid placeholder format - no braces", input: "$VAR",
			envVars: map[string]string{"VAR": "value"}, expected: "$VAR",
		},
		{
			name: "mixed content", input: "prefix-${VAR}-suffix",
			envVars: map[string]string{"VAR": "middle"}, expected: "prefix-middle-suffix",
		},
	}
}

func TestExpand(t *testing.T) {
	for _, testCase := range expandTestCases() {
		t.Run(testCase.name, func(t *testing.T) {
			for key, value := range testCase.envVars {
				t.Setenv(key, value)
			}

			result := envvar.Expand(testCase.input)
			assert.Equal(t, testCase.expected, result)
		})
	}
}
