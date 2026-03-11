package cluster_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	clusterpkg "github.com/devantler-tech/ksail/v5/pkg/cli/cmd/cluster"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatClusterWithTTL_NoTTL(t *testing.T) {
	t.Parallel()

	result := clusterpkg.ExportFormatClusterWithTTL("my-cluster", nil)
	assert.Equal(t, "my-cluster", result)
}

func TestFormatRemainingDuration_HoursAndMinutes(t *testing.T) {
	t.Parallel()

	result := clusterpkg.ExportFormatRemainingDuration(1*time.Hour + 23*time.Minute)
	assert.Equal(t, "1h 23m", result)
}

func TestFormatRemainingDuration_HoursOnly(t *testing.T) {
	t.Parallel()

	result := clusterpkg.ExportFormatRemainingDuration(2 * time.Hour)
	assert.Equal(t, "2h", result)
}

func TestFormatRemainingDuration_MinutesOnly(t *testing.T) {
	t.Parallel()

	result := clusterpkg.ExportFormatRemainingDuration(45 * time.Minute)
	assert.Equal(t, "45m", result)
}

func TestMaybeWaitForTTL_EmptyFlag(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{Use: "create"}
	cmd.Flags().String("ttl", "", "")
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetContext(context.Background())

	clusterCfg := &v1alpha1.Cluster{}

	err := clusterpkg.ExportMaybeWaitForTTL(cmd, "test-cluster", clusterCfg)
	require.NoError(t, err)
}

func TestMaybeWaitForTTL_InvalidDuration(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{Use: "create"}
	cmd.Flags().String("ttl", "", "")
	_ = cmd.Flags().Set("ttl", "not-a-duration")

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetContext(context.Background())

	clusterCfg := &v1alpha1.Cluster{}

	// Invalid duration should not block or attempt deletion; returns nil.
	err := clusterpkg.ExportMaybeWaitForTTL(cmd, "test-cluster", clusterCfg)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "invalid --ttl value")
}

func TestMaybeWaitForTTL_NonPositiveDuration(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{Use: "create"}
	cmd.Flags().String("ttl", "", "")
	_ = cmd.Flags().Set("ttl", "-1h")

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetContext(context.Background())

	clusterCfg := &v1alpha1.Cluster{}

	// Non-positive duration should return immediately without blocking or TTL setup.
	err := clusterpkg.ExportMaybeWaitForTTL(cmd, "test-cluster", clusterCfg)
	require.NoError(t, err)
	assert.Empty(t, buf.String(), "non-positive TTL should produce no output")
}
