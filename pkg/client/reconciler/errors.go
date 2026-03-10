package reconciler

import (
	"context"
	"errors"
)

// IsContextError checks if the error is caused by a context deadline or cancellation.
func IsContextError(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}
