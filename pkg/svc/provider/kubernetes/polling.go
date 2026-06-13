package kubernetes

import (
	"context"
	"errors"
	"fmt"
)

// mapWaitError translates a non-nil error from [wait.PollUntilContextTimeout]
// back to the message each waiter used before adopting the shared poller,
// preserving behavior:
//
//   - a condition-function error (identified by condErr, the last error the
//     condition closure produced) propagates unchanged — even when it wraps a
//     context error;
//   - context cancellation by the poller itself maps to "<cause>: <ctx err>";
//   - a poll timeout maps to the caller's sentinel (timeoutErr).
//
// Passing the captured condErr is what keeps the cancelled-vs-timed-out
// distinction exact: wait.Interrupted alone cannot tell a real cancellation from
// a Get error that merely wraps context.Canceled.
func mapWaitError(err error, condErr error, cause string, timeoutErr error) error {
	// The poller returns the condition's error verbatim when the condition fails;
	// propagate it unchanged regardless of what it wraps.
	if condErr != nil && errors.Is(err, condErr) {
		return condErr
	}

	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("%s: %w", cause, err)
	}

	return timeoutErr
}
