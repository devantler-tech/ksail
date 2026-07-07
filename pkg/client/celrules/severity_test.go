package celrules_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/celrules"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSeverity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected celrules.Severity
		wantErr  bool
	}{
		{name: "empty defaults to error", input: "", expected: celrules.SeverityError},
		{name: "error", input: "error", expected: celrules.SeverityError},
		{name: "warning", input: "warning", expected: celrules.SeverityWarning},
		{name: "warn alias", input: "warn", expected: celrules.SeverityWarning},
		{name: "info", input: "info", expected: celrules.SeverityInfo},
		{name: "uppercase", input: "WARNING", expected: celrules.SeverityWarning},
		{name: "whitespace", input: "  info  ", expected: celrules.SeverityInfo},
		{name: "unknown errors", input: "critical", wantErr: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := celrules.ParseSeverity(testCase.input)

			if testCase.wantErr {
				require.ErrorIs(t, err, celrules.ErrUnknownSeverity)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.expected, got)
		})
	}
}

func TestSeverityString(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "error", celrules.SeverityError.String())
	assert.Equal(t, "warning", celrules.SeverityWarning.String())
	assert.Equal(t, "info", celrules.SeverityInfo.String())
	assert.Equal(t, "Severity(9)", celrules.Severity(9).String())
}
