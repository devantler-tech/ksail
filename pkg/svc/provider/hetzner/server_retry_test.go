package hetzner_test

import (
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
)

func TestShouldRetryError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		err         error
		wantRetry   bool
	}{
		{
			name:      "nil_error",
			err:       nil,
			wantRetry: false,
		},
		{
			name:      "non_hcloud_error",
			err:       errTest,
			wantRetry: false,
		},
		{
			name:      "resource_unavailable",
			err:       hcloud.Error{Code: hcloud.ErrorCodeResourceUnavailable},
			wantRetry: true,
		},
		{
			name:      "conflict",
			err:       hcloud.Error{Code: hcloud.ErrorCodeConflict},
			wantRetry: true,
		},
		{
			name:      "rate_limit_exceeded",
			err:       hcloud.Error{Code: hcloud.ErrorCodeRateLimitExceeded},
			wantRetry: true,
		},
		{
			name:      "locked",
			err:       hcloud.Error{Code: hcloud.ErrorCodeLocked},
			wantRetry: true,
		},
		{
			name:      "placement_error",
			err:       hcloud.Error{Code: hcloud.ErrorCodePlacementError},
			wantRetry: true,
		},
		{
			name:      "resource_limit_exceeded_not_retried",
			err:       hcloud.Error{Code: hcloud.ErrorCodeResourceLimitExceeded},
			wantRetry: false,
		},
		{
			name:      "forbidden_not_retried",
			err:       hcloud.Error{Code: hcloud.ErrorCodeForbidden},
			wantRetry: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := hetzner.ShouldRetryErrorForTest(tc.err)
			assert.Equal(t, tc.wantRetry, got)
		})
	}
}

func TestShouldDisablePlacement(t *testing.T) {
	t.Parallel()

	placementErr := hcloud.Error{Code: hcloud.ErrorCodePlacementError}

	tests := []struct {
		name             string
		err              error
		allowFallback    bool
		placementGroupID int64
		wantDisable      bool
	}{
		{
			name:             "placement_error_with_fallback_and_group",
			err:              placementErr,
			allowFallback:    true,
			placementGroupID: 123,
			wantDisable:      true,
		},
		{
			name:             "placement_error_fallback_disabled",
			err:              placementErr,
			allowFallback:    false,
			placementGroupID: 123,
			wantDisable:      false,
		},
		{
			name:             "placement_error_no_group",
			err:              placementErr,
			allowFallback:    true,
			placementGroupID: 0,
			wantDisable:      false,
		},
		{
			name:             "non_placement_error",
			err:              errTest,
			allowFallback:    true,
			placementGroupID: 123,
			wantDisable:      false,
		},
		{
			name:             "nil_error",
			err:              nil,
			allowFallback:    true,
			placementGroupID: 123,
			wantDisable:      false,
		},
		{
			name:             "rate_limit_error_not_placement",
			err:              hcloud.Error{Code: hcloud.ErrorCodeRateLimitExceeded},
			allowFallback:    true,
			placementGroupID: 123,
			wantDisable:      false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			opts := hetzner.ServerRetryOpts{AllowPlacementFallback: tc.allowFallback}
			got := hetzner.ShouldDisablePlacementForTest(tc.err, opts, tc.placementGroupID)
			assert.Equal(t, tc.wantDisable, got)
		})
	}
}

func TestCalculateRetryDelay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		attempt   int
		wantDelay time.Duration
	}{
		{
			name:      "attempt_1_base_delay",
			attempt:   1,
			wantDelay: 2 * time.Second, // 2s * 2^0
		},
		{
			name:      "attempt_2_doubled",
			attempt:   2,
			wantDelay: 4 * time.Second, // 2s * 2^1
		},
		{
			name:      "attempt_3_quadrupled",
			attempt:   3,
			wantDelay: 8 * time.Second, // 2s * 2^2
		},
		{
			name:      "attempt_4_capped_at_max",
			attempt:   4,
			wantDelay: 10 * time.Second, // 2s * 2^3 = 16s, capped at 10s
		},
		{
			name:      "attempt_10_still_capped",
			attempt:   10,
			wantDelay: 10 * time.Second, // always capped at max
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := hetzner.CalculateRetryDelayForTest(tc.attempt)
			assert.Equal(t, tc.wantDelay, got)
		})
	}
}
