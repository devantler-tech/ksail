package readiness

import "errors"

// ErrTimeoutExceeded is returned when a timeout is exceeded.
var ErrTimeoutExceeded = errors.New("timeout exceeded")

var errUnknownResourceType = errors.New("unknown resource type")
