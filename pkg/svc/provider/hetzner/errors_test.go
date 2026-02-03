package hetzner_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errTest is a static error for test cases.
var errTest = errors.New("test error")

func TestSentinelErrors(t *testing.T) {
	t.Parallel()

	t.Run("ErrResourceUnavailable", func(t *testing.T) {
		t.Parallel()
		require.Error(t, hetzner.ErrResourceUnavailable)
		assert.Contains(t, hetzner.ErrResourceUnavailable.Error(), "unavailable")
	})

	t.Run("ErrPlacementFailed", func(t *testing.T) {
		t.Parallel()
		require.Error(t, hetzner.ErrPlacementFailed)
		assert.Contains(t, hetzner.ErrPlacementFailed.Error(), "placement")
	})

	t.Run("ErrAllLocationsFailed", func(t *testing.T) {
		t.Parallel()
		require.Error(t, hetzner.ErrAllLocationsFailed)
		assert.Contains(t, hetzner.ErrAllLocationsFailed.Error(), "location")
	})
}

//nolint:funlen // Table-driven test with many cases
func TestIsRetryableHetznerError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		err       error
		wantRetry bool
	}{
		{
			name:      "NilError",
			err:       nil,
			wantRetry: false,
		},
		{
			name:      "NonHcloudError",
			err:       errTest,
			wantRetry: false,
		},
		{
			name: "ResourceUnavailable",
			err: hcloud.Error{
				Code:    hcloud.ErrorCodeResourceUnavailable,
				Message: "resource unavailable",
			},
			wantRetry: true,
		},
		{
			name: "Conflict",
			err: hcloud.Error{
				Code:    hcloud.ErrorCodeConflict,
				Message: "conflict",
			},
			wantRetry: true,
		},
		{
			name: "Timeout",
			err: hcloud.Error{
				Code:    hcloud.ErrorCodeTimeout,
				Message: "timeout",
			},
			wantRetry: true,
		},
		{
			name: "RateLimitExceeded",
			err: hcloud.Error{
				Code:    hcloud.ErrorCodeRateLimitExceeded,
				Message: "rate limit",
			},
			wantRetry: true,
		},
		{
			name: "RobotUnavailable",
			err: hcloud.Error{
				Code:    hcloud.ErrorCodeRobotUnavailable,
				Message: "robot unavailable",
			},
			wantRetry: true,
		},
		{
			name: "Locked",
			err: hcloud.Error{
				Code:    hcloud.ErrorCodeLocked,
				Message: "locked",
			},
			wantRetry: true,
		},
		{
			name: "PlacementError_NotRetryable",
			err: hcloud.Error{
				Code:    hcloud.ErrorCodePlacementError,
				Message: "placement error",
			},
			wantRetry: false,
		},
		{
			name: "InvalidInput_NotRetryable",
			err: hcloud.Error{
				Code:    hcloud.ErrorCodeInvalidInput,
				Message: "invalid input",
			},
			wantRetry: false,
		},
		{
			name: "Forbidden_NotRetryable",
			err: hcloud.Error{
				Code:    hcloud.ErrorCodeForbidden,
				Message: "forbidden",
			},
			wantRetry: false,
		},
		{
			name: "WrappedRetryableError",
			err: fmt.Errorf("wrapped: %w", hcloud.Error{
				Code:    hcloud.ErrorCodeConflict,
				Message: "conflict",
			}),
			wantRetry: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := hetzner.IsRetryableHetznerError(testCase.err)
			assert.Equal(t, testCase.wantRetry, result)
		})
	}
}

func TestIsPlacementError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		err             error
		wantIsPlacement bool
	}{
		{
			name:            "NilError",
			err:             nil,
			wantIsPlacement: false,
		},
		{
			name:            "NonHcloudError",
			err:             errTest,
			wantIsPlacement: false,
		},
		{
			name: "ExplicitPlacementError",
			err: hcloud.Error{
				Code:    hcloud.ErrorCodePlacementError,
				Message: "placement error",
			},
			wantIsPlacement: true,
		},
		{
			name: "ResourceUnavailable_TreatedAsPlacement",
			err: hcloud.Error{
				Code:    hcloud.ErrorCodeResourceUnavailable,
				Message: "error during placement (resource_unavailable)",
			},
			wantIsPlacement: true,
		},
		{
			name: "Conflict_NotPlacement",
			err: hcloud.Error{
				Code:    hcloud.ErrorCodeConflict,
				Message: "conflict",
			},
			wantIsPlacement: false,
		},
		{
			name: "WrappedPlacementError",
			err: fmt.Errorf("wrapped: %w", hcloud.Error{
				Code:    hcloud.ErrorCodePlacementError,
				Message: "placement error",
			}),
			wantIsPlacement: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := hetzner.IsPlacementError(testCase.err)
			assert.Equal(t, testCase.wantIsPlacement, result)
		})
	}
}

//nolint:funlen // Table-driven test with many cases
func TestIsResourceLimitError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		err           error
		wantIsLimitEr bool
	}{
		{
			name:          "NilError",
			err:           nil,
			wantIsLimitEr: false,
		},
		{
			name:          "NonHcloudError",
			err:           errTest,
			wantIsLimitEr: false,
		},
		{
			name: "ResourceLimitExceeded",
			err: hcloud.Error{
				Code:    hcloud.ErrorCodeResourceLimitExceeded,
				Message: "quota exceeded",
			},
			wantIsLimitEr: true,
		},
		{
			name: "InvalidInput",
			err: hcloud.Error{
				Code:    hcloud.ErrorCodeInvalidInput,
				Message: "invalid input",
			},
			wantIsLimitEr: true,
		},
		{
			name: "Forbidden",
			err: hcloud.Error{
				Code:    hcloud.ErrorCodeForbidden,
				Message: "forbidden",
			},
			wantIsLimitEr: true,
		},
		{
			name: "Unauthorized",
			err: hcloud.Error{
				Code:    hcloud.ErrorCodeUnauthorized,
				Message: "unauthorized",
			},
			wantIsLimitEr: true,
		},
		{
			name: "ResourceUnavailable_NotLimit",
			err: hcloud.Error{
				Code:    hcloud.ErrorCodeResourceUnavailable,
				Message: "unavailable",
			},
			wantIsLimitEr: false,
		},
		{
			name: "Conflict_NotLimit",
			err: hcloud.Error{
				Code:    hcloud.ErrorCodeConflict,
				Message: "conflict",
			},
			wantIsLimitEr: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := hetzner.IsResourceLimitError(testCase.err)
			assert.Equal(t, testCase.wantIsLimitEr, result)
		})
	}
}

func TestRetryConstants(t *testing.T) {
	t.Parallel()

	t.Run("DefaultMaxServerCreateRetries", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, 3, hetzner.DefaultMaxServerCreateRetries)
	})

	t.Run("DefaultRetryBaseDelay", func(t *testing.T) {
		t.Parallel()
		assert.Positive(t, hetzner.DefaultRetryBaseDelay)
	})

	t.Run("DefaultRetryMaxDelay", func(t *testing.T) {
		t.Parallel()
		assert.Positive(t, hetzner.DefaultRetryMaxDelay)
		assert.GreaterOrEqual(t, hetzner.DefaultRetryMaxDelay, hetzner.DefaultRetryBaseDelay)
	})
}
