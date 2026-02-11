package flags_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v5/pkg/timer"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsBenchmarkEnabled_NilCommand(t *testing.T) {
	t.Parallel()

	_, err := flags.IsBenchmarkEnabled(nil)
	require.Error(t, err)
	snaps.MatchSnapshot(t, err.Error())
}

func TestIsBenchmarkEnabled_FlagFalse(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.Flags().Bool(flags.BenchmarkFlagName, false, "")

	enabled, err := flags.IsBenchmarkEnabled(cmd)
	require.NoError(t, err)
	assert.False(t, enabled)
}

func TestIsBenchmarkEnabled_FlagTrue(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.Flags().Bool(flags.BenchmarkFlagName, true, "")

	enabled, err := flags.IsBenchmarkEnabled(cmd)
	require.NoError(t, err)
	assert.True(t, enabled)
}

func TestIsBenchmarkEnabled_PersistentFlags(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.PersistentFlags().Bool(flags.BenchmarkFlagName, true, "")

	enabled, err := flags.IsBenchmarkEnabled(cmd)
	require.NoError(t, err)
	assert.True(t, enabled)
}

func TestIsBenchmarkEnabled_InheritedFromParent(t *testing.T) {
	t.Parallel()

	parent := &cobra.Command{}
	parent.PersistentFlags().Bool(flags.BenchmarkFlagName, true, "")

	child := &cobra.Command{}
	parent.AddCommand(child)

	enabled, err := flags.IsBenchmarkEnabled(child)
	require.NoError(t, err)
	assert.True(t, enabled)
}

func TestIsBenchmarkEnabled_FlagNotFound(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}

	_, err := flags.IsBenchmarkEnabled(cmd)
	require.Error(t, err)
	snaps.MatchSnapshot(t, err.Error())
}

func TestMaybeTimer_NilCommand(t *testing.T) {
	t.Parallel()

	result := flags.MaybeTimer(nil, timer.New())
	assert.Nil(t, result)
}

func TestMaybeTimer_NilTimer(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.Flags().Bool(flags.BenchmarkFlagName, true, "")

	result := flags.MaybeTimer(cmd, nil)
	assert.Nil(t, result)
}

func TestMaybeTimer_BenchmarkDisabled(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.Flags().Bool(flags.BenchmarkFlagName, false, "")

	result := flags.MaybeTimer(cmd, timer.New())
	assert.Nil(t, result)
}

func TestMaybeTimer_BenchmarkEnabled(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.Flags().Bool(flags.BenchmarkFlagName, true, "")

	tmr := timer.New()
	result := flags.MaybeTimer(cmd, tmr)

	assert.NotNil(t, result)
	assert.Equal(t, tmr, result)
}

func TestMaybeTimer_FlagNotFound(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}

	result := flags.MaybeTimer(cmd, timer.New())
	assert.Nil(t, result)
}
