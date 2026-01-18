package k8s_test

import (
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/k8s"
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
			name:        "ErrKubeconfigPathEmpty is defined",
			err:         k8s.ErrKubeconfigPathEmpty,
			expectedMsg: "kubeconfig path is empty",
		},
		{
			name:        "ErrTimeoutExceeded is defined",
			err:         k8s.ErrTimeoutExceeded,
			expectedMsg: "timeout exceeded",
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
		k8s.ErrKubeconfigPathEmpty,
		k8s.ErrTimeoutExceeded,
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
