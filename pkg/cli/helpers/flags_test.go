package helpers_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/utils/timer"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsTimingEnabled_NilCommand(t *testing.T) {
	t.Parallel()

	_, err := helpers.IsTimingEnabled(nil)
	require.Error(t, err)
	snaps.MatchSnapshot(t, err.Error())
}

func TestIsTimingEnabled_FlagFalse(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.Flags().Bool(helpers.TimingFlagName, false, "")

	enabled, err := helpers.IsTimingEnabled(cmd)
	require.NoError(t, err)
	assert.False(t, enabled)
}

func TestIsTimingEnabled_FlagTrue(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.Flags().Bool(helpers.TimingFlagName, true, "")

	enabled, err := helpers.IsTimingEnabled(cmd)
	require.NoError(t, err)
	assert.True(t, enabled)
}

func TestIsTimingEnabled_PersistentFlags(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.PersistentFlags().Bool(helpers.TimingFlagName, true, "")

	enabled, err := helpers.IsTimingEnabled(cmd)
	require.NoError(t, err)
	assert.True(t, enabled)
}

func TestIsTimingEnabled_InheritedFromParent(t *testing.T) {
	t.Parallel()

	parent := &cobra.Command{}
	parent.PersistentFlags().Bool(helpers.TimingFlagName, true, "")

	child := &cobra.Command{}
	parent.AddCommand(child)

	enabled, err := helpers.IsTimingEnabled(child)
	require.NoError(t, err)
	assert.True(t, enabled)
}

func TestIsTimingEnabled_FlagNotFound(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}

	_, err := helpers.IsTimingEnabled(cmd)
	require.Error(t, err)
	snaps.MatchSnapshot(t, err.Error())
}

func TestMaybeTimer_NilCommand(t *testing.T) {
	t.Parallel()

	result := helpers.MaybeTimer(nil, timer.New())
	assert.Nil(t, result)
}

func TestMaybeTimer_NilTimer(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.Flags().Bool(helpers.TimingFlagName, true, "")

	result := helpers.MaybeTimer(cmd, nil)
	assert.Nil(t, result)
}

func TestMaybeTimer_TimingDisabled(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.Flags().Bool(helpers.TimingFlagName, false, "")

	result := helpers.MaybeTimer(cmd, timer.New())
	assert.Nil(t, result)
}

func TestMaybeTimer_TimingEnabled(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.Flags().Bool(helpers.TimingFlagName, true, "")

	tmr := timer.New()
	result := helpers.MaybeTimer(cmd, tmr)

	assert.NotNil(t, result)
	assert.Equal(t, tmr, result)
}

func TestMaybeTimer_FlagNotFound(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}

	result := helpers.MaybeTimer(cmd, timer.New())
	assert.Nil(t, result)
}
