package cluster_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStripParenthetical_NoSuffix(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "kind", cluster.ExportStripParenthetical("kind"))
}

func TestStripParenthetical_WithSuffix(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "kind", cluster.ExportStripParenthetical("kind (Vanilla)"))
}

func TestStripParenthetical_EmptyString(t *testing.T) {
	t.Parallel()

	assert.Empty(t, cluster.ExportStripParenthetical(""))
}

func TestStripParenthetical_NoClosingParen(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "kind (Vanilla", cluster.ExportStripParenthetical("kind (Vanilla"))
}

func TestFormatRemainingDuration_HoursAndMinutes(t *testing.T) {
	t.Parallel()

	result := cluster.ExportFormatRemainingDuration(1*time.Hour + 23*time.Minute)
	assert.Equal(t, "1h 23m", result)
}

func TestFormatRemainingDuration_HoursOnly(t *testing.T) {
	t.Parallel()

	result := cluster.ExportFormatRemainingDuration(2 * time.Hour)
	assert.Equal(t, "2h", result)
}

func TestFormatRemainingDuration_MinutesOnly(t *testing.T) {
	t.Parallel()

	result := cluster.ExportFormatRemainingDuration(45 * time.Minute)
	assert.Equal(t, "45m", result)
}

func TestFormatRemainingDuration_SubMinute(t *testing.T) {
	t.Parallel()

	result := cluster.ExportFormatRemainingDuration(59 * time.Second)
	assert.Equal(t, "<1m", result)
}

func TestFormatRemainingDuration_TruncatesDown(t *testing.T) {
	t.Parallel()

	// 1h 23m 59s should truncate to 1h 23m, never round up to 1h 24m.
	result := cluster.ExportFormatRemainingDuration(
		1*time.Hour + 23*time.Minute + 59*time.Second,
	)
	assert.Equal(t, "1h 23m", result)
}

func TestMaybeWaitForTTL_EmptyFlag(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{Use: "create"}
	cmd.Flags().String("ttl", "", "")
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetContext(context.Background())

	clusterCfg := &v1alpha1.Cluster{}

	err := cluster.ExportMaybeWaitForTTL(cmd, "test-cluster", clusterCfg)
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
	err := cluster.ExportMaybeWaitForTTL(cmd, "test-cluster", clusterCfg)
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
	err := cluster.ExportMaybeWaitForTTL(cmd, "test-cluster", clusterCfg)
	require.NoError(t, err)
	assert.Empty(t, buf.String(), "non-positive TTL should produce no output")
}

func TestFormatRemainingDuration_LargeDuration(t *testing.T) {
	t.Parallel()

	got := cluster.ExportFormatRemainingDuration(48*time.Hour + 30*time.Minute)
	assert.Equal(t, "48h 30m", got)
}

func TestFormatRemainingDuration_ExactlyZeroMinutes(t *testing.T) {
	t.Parallel()

	got := cluster.ExportFormatRemainingDuration(3 * time.Hour)
	assert.Equal(t, "3h", got)
}

func TestStripParenthetical_SpaceBeforeOpen(t *testing.T) {
	t.Parallel()

	got := cluster.ExportStripParenthetical("Docker (local)")
	assert.Equal(t, "Docker", got)
}

func TestStripParenthetical_DoubleParens(t *testing.T) {
	t.Parallel()

	got := cluster.ExportStripParenthetical("A (b) (c)")
	assert.Equal(t, "A (b)", got)
}
