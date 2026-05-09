package reconciler_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/reconciler"
	"github.com/stretchr/testify/assert"
)

var (
	errReconcilerSomethingWentWrong = errors.New("something went wrong")
	errReconcilerNotContext         = errors.New("not a context error")
)

func TestIsContextError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "deadline exceeded",
			err:      context.DeadlineExceeded,
			expected: true,
		},
		{
			name:     "context canceled",
			err:      context.Canceled,
			expected: true,
		},
		{
			name:     "wrapped deadline exceeded",
			err:      fmt.Errorf("operation failed: %w", context.DeadlineExceeded),
			expected: true,
		},
		{
			name:     "wrapped context canceled",
			err:      fmt.Errorf("reconcile: %w", context.Canceled),
			expected: true,
		},
		{
			name:     "double wrapped deadline exceeded",
			err:      fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", context.DeadlineExceeded)),
			expected: true,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "generic error",
			err:      errReconcilerSomethingWentWrong,
			expected: false,
		},
		{
			name:     "wrapped generic error",
			err:      fmt.Errorf("wrap: %w", errReconcilerNotContext),
			expected: false,
		},
		{
			// The k8s client-go rate limiter returns a plain fmt.Errorf containing
			// "would exceed context deadline" when the context deadline is imminent.
			// This is NOT a wrapped context.DeadlineExceeded, so errors.Is cannot
			// detect it.  IsContextError must treat it as a context error.
			name:     "k8s rate limiter context deadline error",
			err:      fmt.Errorf("client rate limiter Wait returned an error: rate: Wait(n=1) would exceed context deadline"),
			expected: true,
		},
		{
			name:     "wrapped k8s rate limiter context deadline error",
			err:      fmt.Errorf("get flux kustomization: %w", fmt.Errorf("client rate limiter Wait returned an error: rate: Wait(n=1) would exceed context deadline")),
			expected: true,
		},
	}

	for index := range tests {
		t.Run(tests[index].name, func(t *testing.T) {
			t.Parallel()

			result := reconciler.IsContextError(tests[index].err)
			assert.Equal(t, tests[index].expected, result)
		})
	}
}
