package eksprovisioner_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/smithy-go"
	eksprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/eks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func throttlingError() error {
	return &smithy.GenericAPIError{Code: "ThrottlingException", Message: "rate exceeded"}
}

// A control-plane upgrade takes tens of minutes, so one throttled poll must not
// abandon a running upgrade. The SDK retryer only budgets three attempts per
// call; the wait loop owns everything after that until its own deadline.
func TestControlPlaneUpgradeRetriesTransientPollFailures(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		defer cancel()

		api := &upgradeAPI{cluster: upgradeCluster(), result: upgradeResult()}
		api.pollErr = func(polls int) error {
			if polls <= 2 {
				return throttlingError()
			}

			return nil
		}
		api.afterPoll = func() { api.cluster.Version = aws.String("1.35") }

		provisioner := newUpgradeProvisioner(t, api, func(context.Context) error { return nil })
		require.NoError(t, provisioner.UpgradeKubernetes(ctx, "demo", "1.34", "1.35"))
		assert.Equal(t, 3, api.polls)
	})
}

// The counterpart guard: a permanent error must still fail immediately rather
// than be retried for the full 65-minute wait window.
func TestControlPlaneUpgradeFailsFastOnTerminalPollError(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		api := &upgradeAPI{cluster: upgradeCluster(), result: upgradeResult()}
		api.pollErr = func(int) error {
			return &smithy.GenericAPIError{Code: "AccessDeniedException", Message: "denied"}
		}

		provisioner := newUpgradeProvisioner(t, api, func(context.Context) error { return nil })
		err := provisioner.UpgradeKubernetes(t.Context(), "demo", "1.34", "1.35")
		require.Error(t, err)
		require.NotErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, 1, api.polls)
	})
}

// Retrying must not swallow the diagnosis: a wait that expires while polls keep
// failing has to report why they failed, not just that time ran out.
func TestControlPlaneUpgradeSurfacesLastPollErrorAtDeadline(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()

		api := &upgradeAPI{cluster: upgradeCluster(), result: upgradeResult()}
		api.pollErr = func(int) error { return throttlingError() }

		provisioner := newUpgradeProvisioner(t, api, func(context.Context) error { return nil })
		err := provisioner.UpgradeKubernetes(ctx, "demo", "1.34", "1.35")
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Contains(t, err.Error(), "rate exceeded")
		assert.Greater(t, api.polls, 1)
	})
}

// waitDeadlineError wraps the last poll failure with %w, so retaining the
// internal marker there would make a wait that ran out of time classify as a
// retryable poll blip — the opposite of what it is.
func TestControlPlaneUpgradeDeadlineErrorIsNotTransient(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()

		api := &upgradeAPI{cluster: upgradeCluster(), result: upgradeResult()}
		api.pollErr = func(int) error { return throttlingError() }

		provisioner := newUpgradeProvisioner(t, api, func(context.Context) error { return nil })
		err := provisioner.UpgradeKubernetes(ctx, "demo", "1.34", "1.35")
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.False(t, eksprovisioner.IsTransientPollErrorForTest(err),
			"a timed-out wait must not classify as a transient poll failure")
	})
}

// The confirming DescribeCluster that follows a Successful update is subject to
// the same throttling as the poll before it, and by then AWS has already
// finished the upgrade. One bad read must not fail a completed upgrade.
func TestControlPlaneUpgradeRetriesTransientConfirmingRead(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		defer cancel()

		api := &upgradeAPI{cluster: upgradeCluster(), result: upgradeResult()}
		api.afterPoll = func() { api.cluster.Version = aws.String("1.35") }

		throttled := false
		// polls >= 1 selects the confirming read; the pre-upgrade snapshot read
		// happens before any poll and must be left alone.
		api.describeErr = func(_, polls int) error {
			if polls >= 1 && !throttled {
				throttled = true

				return throttlingError()
			}

			return nil
		}

		provisioner := newUpgradeProvisioner(t, api, func(context.Context) error { return nil })
		require.NoError(t, provisioner.UpgradeKubernetes(ctx, "demo", "1.34", "1.35"))
		assert.True(t, throttled, "fixture never injected the throttled confirming read")
		assert.Greater(
			t,
			api.polls,
			1,
			"the wait must poll again after a throttled confirming read",
		)
	})
}

// The counterpart guard: retrying the confirming read must not swallow a
// permanent failure such as a revoked permission.
func TestControlPlaneUpgradeFailsFastOnTerminalConfirmingRead(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		api := &upgradeAPI{cluster: upgradeCluster(), result: upgradeResult()}
		api.afterPoll = func() { api.cluster.Version = aws.String("1.35") }
		api.describeErr = func(_, polls int) error {
			if polls >= 1 {
				return &smithy.GenericAPIError{Code: "AccessDeniedException", Message: "denied"}
			}

			return nil
		}

		provisioner := newUpgradeProvisioner(t, api, func(context.Context) error { return nil })
		err := provisioner.UpgradeKubernetes(t.Context(), "demo", "1.34", "1.35")
		require.Error(t, err)
		require.NotErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, 1, api.polls, "a terminal confirming read must not be retried")
	})
}
