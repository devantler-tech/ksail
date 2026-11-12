package talosprovisioner

import (
	"errors"
	"fmt"
	"io"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
)

// unavailablePolicy says what an availability check does when Hetzner reports a
// server type unavailable in every configured location.
type unavailablePolicy int

const (
	// warnWhenUnavailable reports the result and continues: creating new servers
	// tries every configured location, so a failed create removes nothing.
	warnWhenUnavailable unavailablePolicy = iota
	// stopWhenUnavailable keeps the result fatal. Rolling replacement deletes each
	// old server before creating its replacement, so continuing on a correct
	// report would leave the cluster one node short.
	stopWhenUnavailable
)

// availabilityCheckResult turns the result of a Hetzner server-type availability
// check into the error that should stop provisioning, if any.
//
// Under warnWhenUnavailable, a server type that reports unavailable in every
// configured location does not stop provisioning. Hetzner's per-location
// availability flag has reported a type as unavailable everywhere while creating
// that same type in the same location succeeded minutes earlier (ksail#8100), so
// the flag is not a reliable reason to refuse new servers. The result is reported
// as a warning and the server create call, which already falls back across the
// configured locations, decides. Every other failure, such as a server type that
// does not exist, still stops provisioning, as does any failure under
// stopWhenUnavailable.
func availabilityCheckResult(
	logWriter io.Writer,
	subject string,
	err error,
	policy unavailablePolicy,
) error {
	if err == nil {
		return nil
	}

	if policy == warnWhenUnavailable && errors.Is(err, hetzner.ErrServerTypeUnavailable) {
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
