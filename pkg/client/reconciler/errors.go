package reconciler

import (
	"context"
	"errors"
	"strings"
)

// rateLimiterContextErrSubstr is the substring in the k8s client-go rate limiter
// error that indicates the context deadline would be exceeded.  The rate limiter
// in golang.org/x/time/rate returns a plain fmt.Errorf string (not a wrapped
// context.DeadlineExceeded) when Wait cannot complete within the context deadline.
const rateLimiterContextErrSubstr = "would exceed context deadline"

// IsContextError checks if the error is caused by a context deadline or
// cancellation, including the k8s client-go rate limiter error that surfaces
// when the context is expired or about to expire.
func IsContextError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}

	// The k8s client-go rate limiter (golang.org/x/time/rate) returns a plain
	// fmt.Errorf when the context deadline would be exceeded.  This does not wrap
	// context.DeadlineExceeded, so errors.Is cannot detect it.  Treat the
	// substring match as equivalent to a context deadline error.
	return err != nil && strings.Contains(err.Error(), rateLimiterContextErrSubstr)
}
