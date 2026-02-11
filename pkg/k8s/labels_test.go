package k8s_test

import (
	"fmt"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	"github.com/stretchr/testify/assert"
)

type TestItem struct {
	Labels map[string]string
}

func getTestItemLabels(item TestItem) map[string]string {
	return item.Labels
}

func TestUniqueLabelValues_BasicCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		items    []TestItem
		key      string
		expected []string
	}{
		{
			name:     "empty slice returns empty result",
			items:    []TestItem{},
			key:      "env",
			expected: []string{},
		},
		{
			name: "extracts unique values and sorts them",
			items: []TestItem{
				{Labels: map[string]string{"env": "prod"}},
				{Labels: map[string]string{"env": "dev"}},
				{Labels: map[string]string{"env": "prod"}},
				{Labels: map[string]string{"env": "staging"}},
			},
			key:      "env",
			expected: []string{"dev", "prod", "staging"},
		},
		{
			name: "filters out empty values",
			items: []TestItem{
				{Labels: map[string]string{"env": "prod"}},
				{Labels: map[string]string{"env": ""}},
				{Labels: map[string]string{"env": "dev"}},
			},
			key:      "env",
			expected: []string{"dev", "prod"},
		},
		{
			name: "handles single item",
			items: []TestItem{
				{Labels: map[string]string{"env": "prod"}},
			},
			key:      "env",
			expected: []string{"prod"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := k8s.UniqueLabelValues(tt.items, tt.key, getTestItemLabels)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestUniqueLabelValues_MissingAndNilHandling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		items    []TestItem
		key      string
		expected []string
	}{
		{
			name: "handles missing key",
			items: []TestItem{
				{Labels: map[string]string{"env": "prod"}},
				{Labels: map[string]string{"region": "us-west"}},
				{Labels: map[string]string{"env": "dev"}},
			},
			key:      "env",
			expected: []string{"dev", "prod"},
		},
		{
			name: "handles nil labels",
			items: []TestItem{
				{Labels: map[string]string{"env": "prod"}},
				{Labels: nil},
				{Labels: map[string]string{"env": "dev"}},
			},
			key:      "env",
			expected: []string{"dev", "prod"},
		},
		{
			name: "returns empty when key not found",
			items: []TestItem{
				{Labels: map[string]string{"region": "us-west"}},
				{Labels: map[string]string{"zone": "a"}},
			},
			key:      "env",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := k8s.UniqueLabelValues(tt.items, tt.key, getTestItemLabels)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestUniqueLabelValues_DeduplicationAndSorting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		items    []TestItem
		key      string
		expected []string
	}{
		{
			name: "deduplicates identical values",
			items: []TestItem{
				{Labels: map[string]string{"env": "prod"}},
				{Labels: map[string]string{"env": "prod"}},
				{Labels: map[string]string{"env": "prod"}},
			},
			key:      "env",
			expected: []string{"prod"},
		},
		{
			name: "sorts values alphabetically",
			items: []TestItem{
				{Labels: map[string]string{"env": "zebra"}},
				{Labels: map[string]string{"env": "alpha"}},
				{Labels: map[string]string{"env": "beta"}},
			},
			key:      "env",
			expected: []string{"alpha", "beta", "zebra"},
		},
		{
			name: "handles mixed case values",
			items: []TestItem{
				{Labels: map[string]string{"env": "Production"}},
				{Labels: map[string]string{"env": "development"}},
				{Labels: map[string]string{"env": "STAGING"}},
			},
			key:      "env",
			expected: []string{"Production", "STAGING", "development"},
		},
		{
			name: "handles values with special characters",
			items: []TestItem{
				{Labels: map[string]string{"env": "prod-v1"}},
				{Labels: map[string]string{"env": "dev_test"}},
				{Labels: map[string]string{"env": "staging.2"}},
			},
			key:      "env",
			expected: []string{"dev_test", "prod-v1", "staging.2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := k8s.UniqueLabelValues(tt.items, tt.key, getTestItemLabels)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestUniqueLabelValues_WithDifferentTypes(t *testing.T) {
	t.Parallel()

	t.Run("works with custom struct type", func(t *testing.T) {
		t.Parallel()

		type Pod struct {
			Name      string
			Namespace string
			LabelMap  map[string]string
		}

		pods := []Pod{
			{Name: "pod1", LabelMap: map[string]string{"app": "web"}},
			{Name: "pod2", LabelMap: map[string]string{"app": "api"}},
			{Name: "pod3", LabelMap: map[string]string{"app": "web"}},
		}

		result := k8s.UniqueLabelValues(pods, "app", func(p Pod) map[string]string {
			return p.LabelMap
		})

		assert.Equal(t, []string{"api", "web"}, result)
	})

	t.Run("works with pointer types", func(t *testing.T) {
		t.Parallel()

		type Resource struct {
			Labels map[string]string
		}

		resources := []*Resource{
			{Labels: map[string]string{"tier": "frontend"}},
			{Labels: map[string]string{"tier": "backend"}},
			{Labels: map[string]string{"tier": "frontend"}},
		}

		result := k8s.UniqueLabelValues(resources, "tier", func(r *Resource) map[string]string {
			return r.Labels
		})

		assert.Equal(t, []string{"backend", "frontend"}, result)
	})
}

func TestUniqueLabelValues_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("handles empty key string", func(t *testing.T) {
		t.Parallel()

		items := []TestItem{
			{Labels: map[string]string{"": "value"}},
			{Labels: map[string]string{"env": "prod"}},
		}

		result := k8s.UniqueLabelValues(items, "", getTestItemLabels)
		assert.Equal(t, []string{"value"}, result)
	})

	t.Run("handles very large number of items", func(t *testing.T) {
		t.Parallel()

		items := make([]TestItem, 1000)
		for i := range items {
			items[i] = TestItem{
				Labels: map[string]string{"env": "prod"},
			}
		}

		result := k8s.UniqueLabelValues(items, "env", getTestItemLabels)
		assert.Equal(t, []string{"prod"}, result)
	})

	t.Run("handles many unique values", func(t *testing.T) {
		t.Parallel()

		items := make([]TestItem, 100)
		for i := range items {
			items[i] = TestItem{
				Labels: map[string]string{"id": fmt.Sprintf("value-%03d", i)},
			}
		}

		result := k8s.UniqueLabelValues(items, "id", getTestItemLabels)
		assert.Len(t, result, 100)
		// Verify sorted
		for i := 1; i < len(result); i++ {
			assert.Less(t, result[i-1], result[i], "result should be sorted")
		}
	})
}
