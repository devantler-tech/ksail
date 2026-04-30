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
			name:      "ResourceUnavailable",
			err:       hcloud.Error{Code: hcloud.ErrorCodeResourceUnavailable},
			wantRetry: true,
		},
		{
			name:      "Conflict",
			err:       hcloud.Error{Code: hcloud.ErrorCodeConflict},
			wantRetry: true,
		},
		{
			name:      "RateLimitExceeded",
			err:       hcloud.Error{Code: hcloud.ErrorCodeRateLimitExceeded},
			wantRetry: true,
		},
		{
			name:      "Locked",
			err:       hcloud.Error{Code: hcloud.ErrorCodeLocked},
			wantRetry: true,
		},
		{
			name:      "PlacementError",
			err:       hcloud.Error{Code: hcloud.ErrorCodePlacementError},
			wantRetry: true,
		},
		{
			name:      "ResourceLimitExceeded_NotRetried",
			err:       hcloud.Error{Code: hcloud.ErrorCodeResourceLimitExceeded},
			wantRetry: false,
		},
		{
			name:      "Forbidden_NotRetried",
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
			name:             "PlacementError_WithFallbackAndGroup",
			err:              placementErr,
			allowFallback:    true,
			placementGroupID: 123,
			wantDisable:      true,
		},
		{
			name:             "PlacementError_FallbackDisabled",
			err:              placementErr,
			allowFallback:    false,
			placementGroupID: 123,
			wantDisable:      false,
		},
		{
			name:             "PlacementError_NoGroup",
			err:              placementErr,
			allowFallback:    true,
			placementGroupID: 0,
			wantDisable:      false,
		},
		{
			name:             "NonPlacementError",
			err:              errTest,
			allowFallback:    true,
			placementGroupID: 123,
			wantDisable:      false,
		},
		{
			name:             "NilError",
			err:              nil,
			allowFallback:    true,
			placementGroupID: 123,
			wantDisable:      false,
		},
		{
			name:             "RateLimitError_NotPlacement",
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
			name:      "Attempt1_BaseDelay",
			attempt:   1,
			wantDelay: 2 * time.Second, // 2s * 2^0
		},
		{
			name:      "Attempt2_Doubled",
			attempt:   2,
			wantDelay: 4 * time.Second, // 2s * 2^1
		},
		{
			name:      "Attempt3_Quadrupled",
			attempt:   3,
			wantDelay: 8 * time.Second, // 2s * 2^2
		},
		{
			name:      "Attempt4_CappedAtMax",
			attempt:   4,
			wantDelay: 10 * time.Second, // 2s * 2^3 = 16s, capped at 10s
		},
		{
			name:      "Attempt10_StillCapped",
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
