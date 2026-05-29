package envvar_test

import (
	"os"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/envvar"
	"github.com/stretchr/testify/assert"
)

// Note: Tests using t.Setenv cannot be run in parallel, so we run them sequentially.

const durationTestEnvVar = "KSAIL_TEST_DURATION"

func TestDuration(t *testing.T) {
	fallback := 10 * time.Minute

	testCases := []struct {
		name     string
		set      bool
		value    string
		expected time.Duration
	}{
		{name: "unset uses fallback", set: false, value: "", expected: fallback},
		{name: "empty uses fallback", set: true, value: "", expected: fallback},
		{name: "valid minutes", set: true, value: "15m", expected: 15 * time.Minute},
		{name: "valid seconds", set: true, value: "1200s", expected: 1200 * time.Second},
		{name: "valid compound", set: true, value: "1h30m", expected: 90 * time.Minute},
		{name: "unparseable uses fallback", set: true, value: "not-a-duration", expected: fallback},
		{name: "missing unit uses fallback", set: true, value: "20", expected: fallback},
		{name: "zero uses fallback", set: true, value: "0", expected: fallback},
		{name: "negative uses fallback", set: true, value: "-5m", expected: fallback},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// Register cleanup to restore the original value, then put the variable
			// into the desired state for this case.
			t.Setenv(durationTestEnvVar, "placeholder")

			if testCase.set {
				t.Setenv(durationTestEnvVar, testCase.value)
			} else {
				_ = os.Unsetenv(durationTestEnvVar)
			}

			got := envvar.Duration(durationTestEnvVar, fallback)
			assert.Equal(t, testCase.expected, got)
		})
	}
}
