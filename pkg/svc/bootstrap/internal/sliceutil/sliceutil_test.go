package sliceutil_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/internal/sliceutil"
	"github.com/stretchr/testify/assert"
)

func TestSortedNonEmpty(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		input  []string
		expect []string
	}{
		{name: "nil input returns nil", input: nil, expect: nil},
		{name: "all-empty input returns nil", input: []string{"", ""}, expect: nil},
		{
			name:   "drops empties and sorts",
			input:  []string{"b", "", "a", "c", ""},
			expect: []string{"a", "b", "c"},
		},
		{
			name:   "already sorted stays sorted",
			input:  []string{"a", "b"},
			expect: []string{"a", "b"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := sliceutil.SortedNonEmpty(testCase.input)

			assert.Equal(t, testCase.expect, got)
		})
	}
}

func TestSortedNonEmptyDoesNotMutateInput(t *testing.T) {
	t.Parallel()

	input := []string{"b", "", "a"}

	_ = sliceutil.SortedNonEmpty(input)

	assert.Equal(t, []string{"b", "", "a"}, input)
}
