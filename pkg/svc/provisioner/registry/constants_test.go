package registry_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
)

func TestSanitizeRepoName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "simple directory", input: "k8s", expected: "k8s"},
		{name: "directory with dots", input: ".github", expected: "github"},
		{
			name:     "path with slashes",
			input:    ".github/fixtures/reconcile-test",
			expected: "github-fixtures-reconcile-test",
		},
		{name: "uppercase to lowercase", input: "MyProject", expected: "myproject"},
		{name: "spaces to hyphens", input: "my project", expected: "my-project"},
		{name: "consecutive chars collapsed", input: "my...project", expected: "my-project"},
		{name: "trim leading trailing hyphens", input: "---my-project---", expected: "my-project"},
		{name: "empty returns default", input: "", expected: registry.DefaultRepoName},
		{name: "whitespace returns default", input: "   ", expected: registry.DefaultRepoName},
		{name: "numeric preserved", input: "project123", expected: "project123"},
		{name: "complex path", input: "path/to/my-workloads", expected: "path-to-my-workloads"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := registry.SanitizeRepoName(testCase.input)
			if result != testCase.expected {
				t.Errorf(
					"SanitizeRepoName(%q) = %q, want %q",
					testCase.input,
					result,
					testCase.expected,
				)
			}
		})
	}
}
