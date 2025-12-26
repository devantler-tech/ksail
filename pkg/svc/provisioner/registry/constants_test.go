package registry

import "testing"

func TestSanitizeRepoName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple directory",
			input:    "k8s",
			expected: "k8s",
		},
		{
			name:     "directory with dots",
			input:    ".github",
			expected: "github",
		},
		{
			name:     "path with slashes",
			input:    ".github/fixtures/reconcile-test",
			expected: "github-fixtures-reconcile-test",
		},
		{
			name:     "uppercase converted to lowercase",
			input:    "MyProject",
			expected: "myproject",
		},
		{
			name:     "spaces replaced with hyphens",
			input:    "my project",
			expected: "my-project",
		},
		{
			name:     "consecutive special chars collapsed",
			input:    "my...project",
			expected: "my-project",
		},
		{
			name:     "leading and trailing hyphens trimmed",
			input:    "---my-project---",
			expected: "my-project",
		},
		{
			name:     "empty string returns default",
			input:    "",
			expected: DefaultRepoName,
		},
		{
			name:     "whitespace only returns default",
			input:    "   ",
			expected: DefaultRepoName,
		},
		{
			name:     "numeric values preserved",
			input:    "project123",
			expected: "project123",
		},
		{
			name:     "complex path",
			input:    "path/to/my-workloads",
			expected: "path-to-my-workloads",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := SanitizeRepoName(tc.input)
			if result != tc.expected {
				t.Errorf("SanitizeRepoName(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}
