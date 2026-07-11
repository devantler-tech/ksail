package mirror

import (
	"context"
	"time"
)

// DefaultSteerImageForVersion exposes defaultSteerImage to the black-box test
// package so the version-pinned steer image derivation can be exercised for
// stamped release builds and unstamped/dev builds alike.
func DefaultSteerImageForVersion(version string) string {
	return defaultSteerImage(version)
}

// WatchSessionLiveness exposes watchSessionLiveness to the black-box test
// package so the ksail#6040 client-liveness watchdog can be exercised with
// test-sized timeouts instead of the production constant.
func WatchSessionLiveness(
	ctx context.Context,
	session *TunnelSession,
	timeout time.Duration,
	expire context.CancelFunc,
) {
	watchSessionLiveness(ctx, session, timeout, expire)
}
