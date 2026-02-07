package annotations_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
)

func TestAnnotationConstants_AreNonEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
	}{
		{
			name:  "AnnotationExclude",
			value: annotations.AnnotationExclude,
		},
		{
			name:  "AnnotationDescription",
			value: annotations.AnnotationDescription,
		},
		{
			name:  "AnnotationPermission",
			value: annotations.AnnotationPermission,
		},
		{
			name:  "AnnotationConsolidate",
			value: annotations.AnnotationConsolidate,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if test.value == "" {
				t.Errorf("%s should not be empty", test.name)
			}
		})
	}
}

func TestAnnotationConstants_HaveCorrectPrefix(t *testing.T) {
	t.Parallel()

	const expectedPrefix = "ai.toolgen."

	tests := []struct {
		name  string
		value string
	}{
		{
			name:  "AnnotationExclude",
			value: annotations.AnnotationExclude,
		},
		{
			name:  "AnnotationDescription",
			value: annotations.AnnotationDescription,
		},
		{
			name:  "AnnotationPermission",
			value: annotations.AnnotationPermission,
		},
		{
			name:  "AnnotationConsolidate",
			value: annotations.AnnotationConsolidate,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if len(test.value) < len(expectedPrefix) {
				t.Errorf(
					"%s value %q shorter than expected prefix %q",
					test.name, test.value, expectedPrefix,
				)

				return
			}

			if test.value[:len(expectedPrefix)] != expectedPrefix {
				t.Errorf("%s = %q; want prefix %q", test.name, test.value, expectedPrefix)
			}
		})
	}
}

func TestAnnotationConstants_AreDistinct(t *testing.T) {
	t.Parallel()

	values := []string{
		annotations.AnnotationExclude,
		annotations.AnnotationDescription,
		annotations.AnnotationPermission,
		annotations.AnnotationConsolidate,
	}

	seen := make(map[string]bool)
	for _, v := range values {
		if seen[v] {
			t.Errorf("duplicate annotation value: %s", v)
		}

		seen[v] = true
	}
}

func TestAnnotationConstants_HaveExpectedValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{
			name:     "AnnotationExclude",
			got:      annotations.AnnotationExclude,
			expected: "ai.toolgen.exclude",
		},
		{
			name:     "AnnotationDescription",
			got:      annotations.AnnotationDescription,
			expected: "ai.toolgen.description",
		},
		{
			name:     "AnnotationPermission",
			got:      annotations.AnnotationPermission,
			expected: "ai.toolgen.permission",
		},
		{
			name:     "AnnotationConsolidate",
			got:      annotations.AnnotationConsolidate,
			expected: "ai.toolgen.consolidate",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if test.got != test.expected {
				t.Errorf("%s = %q; want %q", test.name, test.got, test.expected)
			}
		})
	}
}
