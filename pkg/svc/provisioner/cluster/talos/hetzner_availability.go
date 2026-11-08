package talosprovisioner

import (
	"errors"
	"fmt"
	"io"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
)

// availabilityCheckResult turns the result of a Hetzner server-type availability
// check into the error that should stop provisioning, if any.
//
// A server type that reports unavailable in every configured location does not
// stop provisioning. Hetzner's per-location availability flag has reported a type
// as unavailable everywhere while creating that same type in the same location
// succeeded minutes earlier (ksail#8100), so the flag is not a reliable reason to
// refuse. The result is reported as a warning and the server create call, which
// already falls back across the configured locations, decides. Every other
// failure, such as a server type that does not exist, still stops provisioning.
func availabilityCheckResult(logWriter io.Writer, subject string, err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, hetzner.ErrServerTypeUnavailable) {
		if logWriter != nil {
			_, _ = fmt.Fprintf(
				logWriter,
				"  ⚠ Hetzner reports %s as unavailable (%v); continuing, because server creation tries every configured location\n",
				subject,
				err,
			)
		}

		return nil
	}

	return fmt.Errorf("server availability check failed for %s: %w", subject, err)
}
