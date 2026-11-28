package talosprovisioner

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/siderolabs/talos/pkg/conditions"
	"github.com/stretchr/testify/require"
)

var errTestEtcdMemberUnhealthy = errors.New("etcd member is unhealthy")

func TestCheckReporterPrintsLastAssertionErrorOnce(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer

	reporter := &checkReporter{writer: &output}
	condition := conditions.PollingCondition(
		"etcd to be healthy",
		func(context.Context) error { return errTestEtcdMemberUnhealthy },
		time.Millisecond,
	)

	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()

	require.ErrorIs(t, condition.Wait(ctx), context.DeadlineExceeded)
	reporter.Update(condition)
	reporter.Update(condition)

	require.Equal(t, "    etcd to be healthy: etcd member is unhealthy\n", output.String())
}
