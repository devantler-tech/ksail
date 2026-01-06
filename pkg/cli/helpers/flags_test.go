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

func TestIsTimingEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		setupCmd    func() *cobra.Command
		wantEnabled bool
		wantErr     bool
	}{
		{
			name: "returns error for nil command",
			setupCmd: func() *cobra.Command {
				return nil
			},
			wantErr: true,
		},
		{
			name: "returns false when timing flag is false",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{}
				cmd.Flags().Bool(helpers.TimingFlagName, false, "")

				return cmd
			},
			wantEnabled: false,
		},
		{
			name: "returns true when timing flag is true",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{}
				cmd.Flags().Bool(helpers.TimingFlagName, true, "")

				return cmd
			},
			wantEnabled: true,
		},
		{
			name: "finds timing in persistent flags",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{}
				cmd.PersistentFlags().Bool(helpers.TimingFlagName, true, "")

				return cmd
			},
			wantEnabled: true,
		},
		{
			name: "finds timing in inherited flags from parent",
			setupCmd: func() *cobra.Command {
				parent := &cobra.Command{}
				parent.PersistentFlags().Bool(helpers.TimingFlagName, true, "")

				child := &cobra.Command{}
				parent.AddCommand(child)

				return child
			},
			wantEnabled: true,
		},
		{
			name: "returns error when flag not found",
			setupCmd: func() *cobra.Command {
				return &cobra.Command{}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := tt.setupCmd()
			enabled, err := helpers.IsTimingEnabled(cmd)

			if tt.wantErr {
				require.Error(t, err)
				snaps.MatchSnapshot(t, err.Error())

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantEnabled, enabled)
		})
	}
}

func TestMaybeTimer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		setupCmd func() *cobra.Command
		timer    timer.Timer
		wantNil  bool
	}{
		{
			name: "returns nil for nil command",
			setupCmd: func() *cobra.Command {
				return nil
			},
			timer:   timer.New(),
			wantNil: true,
		},
		{
			name: "returns nil for nil timer",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{}
				cmd.Flags().Bool(helpers.TimingFlagName, true, "")

				return cmd
			},
			timer:   nil,
			wantNil: true,
		},
		{
			name: "returns nil when timing disabled",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{}
				cmd.Flags().Bool(helpers.TimingFlagName, false, "")

				return cmd
			},
			timer:   timer.New(),
			wantNil: true,
		},
		{
			name: "returns timer when timing enabled",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{}
				cmd.Flags().Bool(helpers.TimingFlagName, true, "")

				return cmd
			},
			timer:   timer.New(),
			wantNil: false,
		},
		{
			name: "returns nil when flag not found",
			setupCmd: func() *cobra.Command {
				return &cobra.Command{}
			},
			timer:   timer.New(),
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := tt.setupCmd()
			result := helpers.MaybeTimer(cmd, tt.timer)

			if tt.wantNil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				assert.Equal(t, tt.timer, result)
			}
		})
	}
}
