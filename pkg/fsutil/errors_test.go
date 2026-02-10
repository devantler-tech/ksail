package fsutil_test

import (
	"fmt"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/fsutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrorVariables(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		err         error
		expectedMsg string
	}{
		{
			name:        "ErrPathOutsideBase is defined",
			err:         fsutil.ErrPathOutsideBase,
			expectedMsg: "invalid path: file is outside base directory",
		},
		{
			name:        "ErrEmptyOutputPath is defined",
			err:         fsutil.ErrEmptyOutputPath,
			expectedMsg: "output path cannot be empty",
		},
		{
			name:        "ErrBasePath is defined",
			err:         fsutil.ErrBasePath,
			expectedMsg: "base path cannot be empty",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Error(t, testCase.err)
			assert.Equal(t, testCase.expectedMsg, testCase.err.Error())
		})
	}
}

func TestErrorsAreDistinct(t *testing.T) {
	t.Parallel()

	allErrors := []error{
		fsutil.ErrPathOutsideBase,
		fsutil.ErrEmptyOutputPath,
		fsutil.ErrBasePath,
	}

	// Verify all errors are distinct from each other
	for index := range allErrors {
		for innerIndex := index + 1; innerIndex < len(allErrors); innerIndex++ {
			assert.NotErrorIs(
				t,
				allErrors[index], allErrors[innerIndex],
				"errors at index %d and %d should be distinct",
				index,
				innerIndex,
			)
		}
	}
}

func TestErrorsCanBeWrapped(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		sentinel error
	}{
		{name: "ErrPathOutsideBase can be wrapped", sentinel: fsutil.ErrPathOutsideBase},
		{name: "ErrEmptyOutputPath can be wrapped", sentinel: fsutil.ErrEmptyOutputPath},
		{name: "ErrBasePath can be wrapped", sentinel: fsutil.ErrBasePath},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// Wrap the error using fmt.Errorf with %w
			wrapped := fmt.Errorf("context: %w", testCase.sentinel)

			// Verify error wrapping works correctly with errors.Is
			assert.ErrorIs(t, wrapped, testCase.sentinel)
		})
	}
}
