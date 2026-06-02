package vclusterprovisioner_test

import (
	"os"
	"testing"
	"time"

	vclusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/vcluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVClusterReadyTimeout(t *testing.T) {
	// Uses t.Setenv, so it cannot run in parallel.
	t.Run("defaults to ten minutes when unset", func(t *testing.T) {
		t.Setenv("KSAIL_NESTED_READY_TIMEOUT", "placeholder")
		require.NoError(t, os.Unsetenv("KSAIL_NESTED_READY_TIMEOUT"))

		assert.Equal(t, 10*time.Minute, vclusterprovisioner.VClusterReadyTimeoutForTest())
	})

	t.Run("honors KSAIL_NESTED_READY_TIMEOUT override", func(t *testing.T) {
		t.Setenv("KSAIL_NESTED_READY_TIMEOUT", "15m")

		assert.Equal(t, 15*time.Minute, vclusterprovisioner.VClusterReadyTimeoutForTest())
	})
}
