package eksprovisioner_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/smithy-go"
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
