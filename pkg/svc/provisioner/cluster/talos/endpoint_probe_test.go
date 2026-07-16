package talosprovisioner_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
)

// TestVerifiedEndpointIP_SameAddressSkipsProbe pins the short-circuit: when
// candidate and fallback already agree there is nothing to verify, so plain
// node-endpoint clusters never pay for a probe.
func TestVerifiedEndpointIP_SameAddressSkipsProbe(t *testing.T) {
	t.Parallel()

	probes := 0
	provisioner := talosprovisioner.NewProvisioner(nil, nil).
		WithAPIEndpointReachabilityCheckForTest(
			func(context.Context, string, time.Duration) error {
				probes++

				return nil
			},
		)

	got := provisioner.VerifiedEndpointIPForTest(
		t.Context(), "203.0.113.5", "203.0.113.5", time.Second,
	)

	assert.Equal(t, "203.0.113.5", got)
	assert.Equal(t, 0, probes, "identical candidate and fallback must not probe")
}

// TestVerifiedEndpointIP_ReachableCandidateAdopted verifies a candidate the
// probe confirms reachable is adopted unchanged.
func TestVerifiedEndpointIP_ReachableCandidateAdopted(t *testing.T) {
	t.Parallel()

	logs := &bytes.Buffer{}
	provisioner := talosprovisioner.NewProvisioner(nil, nil).
		WithLogWriter(logs).
		WithAPIEndpointReachabilityCheckForTest(
			func(context.Context, string, time.Duration) error { return nil },
		)

	got := provisioner.VerifiedEndpointIPForTest(
		t.Context(), "192.0.2.10", "203.0.113.5", time.Second,
	)

	assert.Equal(t, "192.0.2.10", got)
	assert.Empty(t, logs.String(), "a reachable endpoint must not warn")
}

// TestVerifiedEndpointIP_UnreachableCandidateFallsBackWithWarning pins the
// ksail#6070 guard at the unit level: a dead candidate yields the fallback and
// a warning naming both addresses so the operator can see the substitution.
func TestVerifiedEndpointIP_UnreachableCandidateFallsBackWithWarning(t *testing.T) {
	t.Parallel()

	logs := &bytes.Buffer{}
	provisioner := talosprovisioner.NewProvisioner(nil, nil).
		WithLogWriter(logs).
		WithAPIEndpointReachabilityCheckForTest(
			func(context.Context, string, time.Duration) error {
				return errEndpointProbeFailed
			},
		)

	got := provisioner.VerifiedEndpointIPForTest(
		t.Context(), "192.0.2.10", "203.0.113.5", time.Second,
	)

	assert.Equal(t, "203.0.113.5", got)
	assert.Contains(t, logs.String(), "192.0.2.10")
	assert.Contains(t, logs.String(), "203.0.113.5")
}
