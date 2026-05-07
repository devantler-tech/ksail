package hetzner_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/stretchr/testify/assert"
)

func TestSchematicLabelValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		wantLen  int
		wantFull bool // whether the output should equal the input
	}{
		{
			name:     "short value unchanged",
			input:    "abc123",
			wantLen:  6,
			wantFull: true,
		},
		{
			name:     "63-char value unchanged",
			input:    strings.Repeat("a", 63),
			wantLen:  63,
			wantFull: true,
		},
		{
			name:     "64-char SHA256 truncated to 63",
			input:    "e187c9b90f773cd8c84e5a3265c5554ee787b2fe67b508d9f955e90e7ae8c96c",
			wantLen:  63,
			wantFull: false,
		},
		{
			name:     "longer value truncated to 63",
			input:    strings.Repeat("b", 100),
			wantLen:  63,
			wantFull: false,
		},
		{
			name:     "empty value unchanged",
			input:    "",
			wantLen:  0,
			wantFull: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := hetzner.SchematicLabelValue(testCase.input)

			assert.Len(t, got, testCase.wantLen)

			if testCase.wantFull {
				assert.Equal(t, testCase.input, got)
			} else {
				assert.Equal(t, testCase.input[:63], got)
			}
		})
	}
}
