package strutil_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/strutil"
	"github.com/stretchr/testify/assert"
)

func TestSnakeCaseToTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "snake_case tool name",
			input:    "cluster_create",
			expected: "Cluster Create",
		},
		{
			name:     "underscore suffix like consolidated tool",
			input:    "workload_read",
			expected: "Workload Read",
		},
		{
			name:     "space-separated words",
			input:    "cluster create",
			expected: "Cluster Create",
		},
		{
			name:     "single word",
			input:    "cipher",
			expected: "Cipher",
		},
		{
			name:     "already title case",
			input:    "Cluster Create",
			expected: "Cluster Create",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "multiple underscores",
			input:    "cluster_switch_context",
			expected: "Cluster Switch Context",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := strutil.SnakeCaseToTitle(testCase.input)
			assert.Equal(t, testCase.expected, result)
		})
	}
}
