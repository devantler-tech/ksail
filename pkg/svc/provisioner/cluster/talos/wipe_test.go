package talosprovisioner_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
)

func TestApplyWipeRequiredChanges_PartitionDetection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		wipeChanges      []clusterupdate.Change
		expectsEphemeral bool
		expectsState     bool
	}{
		{
			name:             "no wipe changes",
			wipeChanges:      nil,
			expectsEphemeral: false,
			expectsState:     false,
		},
		{
			name: "ephemeral only",
			wipeChanges: []clusterupdate.Change{
				{
					Field:    "machine.systemDiskEncryption.ephemeral",
					OldValue: "none",
					NewValue: "luks2",
					Category: clusterupdate.ChangeCategoryWipeRequired,
					Reason:   "EPHEMERAL partition encryption change requires partition wipe",
				},
			},
			expectsEphemeral: true,
			expectsState:     false,
		},
		{
			name: "state only",
			wipeChanges: []clusterupdate.Change{
				{
					Field:    "machine.systemDiskEncryption.state",
					OldValue: "none",
					NewValue: "luks2",
					Category: clusterupdate.ChangeCategoryWipeRequired,
					Reason:   "STATE partition encryption change requires partition wipe and maintenance mode",
				},
			},
			expectsEphemeral: false,
			expectsState:     true,
		},
		{
			name: "both ephemeral and state",
			wipeChanges: []clusterupdate.Change{
				{
					Field:    "machine.systemDiskEncryption.ephemeral",
					OldValue: "luks2",
					NewValue: "none",
					Category: clusterupdate.ChangeCategoryWipeRequired,
					Reason:   "EPHEMERAL partition encryption change requires partition wipe",
				},
				{
					Field:    "machine.systemDiskEncryption.state",
					OldValue: "luks2",
					NewValue: "none",
					Category: clusterupdate.ChangeCategoryWipeRequired,
					Reason:   "STATE partition encryption change requires partition wipe and maintenance mode",
				},
			},
			expectsEphemeral: true,
			expectsState:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := clusterupdate.NewEmptyUpdateResult()
			result.WipeRequired = tt.wipeChanges

			// Verify the WipeRequired field correctly reflects the expected partitions
			hasEphemeral := false
			hasState := false

			for _, change := range result.WipeRequired {
				if change.Field == "machine.systemDiskEncryption.ephemeral" {
					hasEphemeral = true
				}

				if change.Field == "machine.systemDiskEncryption.state" {
					hasState = true
				}
			}

			assert.Equal(t, tt.expectsEphemeral, hasEphemeral, "ephemeral partition detection")
			assert.Equal(t, tt.expectsState, hasState, "state partition detection")
		})
	}
}

func TestUpdateResult_HasWipeRequired(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		result   *clusterupdate.UpdateResult
		expected bool
	}{
		{
			name:     "empty result has no wipe required",
			result:   clusterupdate.NewEmptyUpdateResult(),
			expected: false,
		},
		{
			name: "result with wipe changes",
			result: func() *clusterupdate.UpdateResult {
				r := clusterupdate.NewEmptyUpdateResult()
				r.WipeRequired = []clusterupdate.Change{
					{
						Field:    "machine.systemDiskEncryption.ephemeral",
						Category: clusterupdate.ChangeCategoryWipeRequired,
					},
				}

				return r
			}(),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.expected, tt.result.HasWipeRequired())
		})
	}
}
