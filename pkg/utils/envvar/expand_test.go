package envvar_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/utils/envvar"
	"github.com/stretchr/testify/assert"
)

func TestExpand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		envVars  map[string]string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			envVars:  nil,
			expected: "",
		},
		{
			name:     "no placeholders",
			input:    "hello world",
			envVars:  nil,
			expected: "hello world",
		},
		{
			name:  "single placeholder with value",
			input: "hello ${NAME}",
			envVars: map[string]string{
				"NAME": "world",
			},
			expected: "hello world",
		},
		{
			name:     "single placeholder without value",
			input:    "hello ${MISSING}",
			envVars:  nil,
			expected: "hello ",
		},
		{
			name:  "multiple placeholders",
			input: "${GREETING} ${NAME}!",
			envVars: map[string]string{
				"GREETING": "Hello",
				"NAME":     "World",
			},
			expected: "Hello World!",
		},
		{
			name:  "placeholder with underscores",
			input: "${MY_VAR_NAME}",
			envVars: map[string]string{
				"MY_VAR_NAME": "value",
			},
			expected: "value",
		},
		{
			name:  "placeholder with numbers",
			input: "${VAR123}",
			envVars: map[string]string{
				"VAR123": "numeric",
			},
			expected: "numeric",
		},
		{
			name:     "invalid placeholder format - no braces",
			input:    "$VAR",
			envVars:  map[string]string{"VAR": "value"},
			expected: "$VAR",
		},
		{
			name:  "mixed content",
			input: "prefix-${VAR}-suffix",
			envVars: map[string]string{
				"VAR": "middle",
			},
			expected: "prefix-middle-suffix",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Set environment variables for this test
			for key, value := range tc.envVars {
				t.Setenv(key, value)
			}

			result := envvar.Expand(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}
