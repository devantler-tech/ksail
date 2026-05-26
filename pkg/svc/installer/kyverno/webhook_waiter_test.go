package kyvernoinstaller_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	kyvernoinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/kyverno"
	"github.com/stretchr/testify/assert"
)

// Static sentinel errors for the table-driven classifier test (satisfies err113).
var (
	errRateLimiterDeadline = errors.New(
		"client rate limiter Wait returned an error: rate: Wait(n=1) would exceed context deadline",
	)
	errArbitrary      = errors.New("any error")
	errConnRefused    = errors.New("connection refused")
	errWrappedTimeout = fmt.Errorf("polling: %w", context.DeadlineExceeded)
)

func TestIsDeadlineError(t *testing.T) {
	t.Parallel()

	liveCtx := context.Background()

	doneCtx, cancel := context.WithCancel(context.Background())
	cancel()

	tests := map[string]struct {
		ctx  context.Context //nolint:containedctx // table-driven input for the classifier
		err  error
		want bool
	}{
		"wrapped context.DeadlineExceeded": {
			ctx:  liveCtx,
			err:  errWrappedTimeout,
			want: true,
		},
		"rate limiter would-exceed-deadline (not a context error)": {
			ctx:  liveCtx,
			err:  errRateLimiterDeadline,
			want: true,
		},
		"context already done": {
			ctx:  doneCtx,
			err:  errArbitrary,
			want: true,
		},
		"unrelated error with live context": {
			ctx:  liveCtx,
			err:  errConnRefused,
			want: false,
		},
	}

	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := kyvernoinstaller.IsDeadlineErrorForTest(testCase.ctx, testCase.err)
			assert.Equal(t, testCase.want, got)
		})
	}
}
