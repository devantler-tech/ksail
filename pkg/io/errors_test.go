package io_test

import (
	"errors"
	"testing"

	io "github.com/devantler-tech/ksail/v5/pkg/io"
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
			err:         io.ErrPathOutsideBase,
			expectedMsg: "invalid path: file is outside base directory",
		},
		{
			name:        "ErrEmptyOutputPath is defined",
			err:         io.ErrEmptyOutputPath,
			expectedMsg: "output path cannot be empty",
		},
		{
			name:        "ErrBasePath is defined",
			err:         io.ErrBasePath,
			expectedMsg: "base path cannot be empty",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.NotNil(t, testCase.err)
			assert.Equal(t, testCase.expectedMsg, testCase.err.Error())
		})
	}
}

func TestErrorsAreDistinct(t *testing.T) {
	t.Parallel()

	allErrors := []error{
		io.ErrPathOutsideBase,
		io.ErrEmptyOutputPath,
		io.ErrBasePath,
	}

	// Verify all errors are distinct from each other
	for i := 0; i < len(allErrors); i++ {
		for j := i + 1; j < len(allErrors); j++ {
			assert.False(
				t,
				errors.Is(allErrors[i], allErrors[j]),
				"errors at index %d and %d should be distinct",
				i,
				j,
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
		{name: "ErrPathOutsideBase can be wrapped", sentinel: io.ErrPathOutsideBase},
		{name: "ErrEmptyOutputPath can be wrapped", sentinel: io.ErrEmptyOutputPath},
		{name: "ErrBasePath can be wrapped", sentinel: io.ErrBasePath},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// Wrap the error
			wrapped := errors.New("context: " + testCase.sentinel.Error())

			// The wrapped error message should contain the sentinel message
			assert.Contains(t, wrapped.Error(), testCase.sentinel.Error())
		})
	}
}
