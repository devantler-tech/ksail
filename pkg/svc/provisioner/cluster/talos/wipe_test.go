package talosprovisioner_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
)

func TestPartitionWipeDecision_EphemeralOnly(t *testing.T) {
	t.Parallel()

	changes := []clusterupdate.Change{
		{
			Field:    talosprovisioner.FieldEphemeralEncryption,
			OldValue: "none",
			NewValue: "luks2",
			Category: clusterupdate.ChangeCategoryWipeRequired,
		},
	}

	ephemeral, state := talosprovisioner.PartitionWipeDecisionForTest(changes)
	assert.True(t, ephemeral, "should detect ephemeral wipe")
	assert.False(t, state, "should not detect state wipe")
}

func TestPartitionWipeDecision_StateOnly(t *testing.T) {
	t.Parallel()

	changes := []clusterupdate.Change{
		{
			Field:    talosprovisioner.FieldStateEncryption,
			OldValue: "none",
			NewValue: "luks2",
			Category: clusterupdate.ChangeCategoryWipeRequired,
		},
	}

	ephemeral, state := talosprovisioner.PartitionWipeDecisionForTest(changes)
	assert.False(t, ephemeral, "should not detect ephemeral wipe")
	assert.True(t, state, "should detect state wipe")
}

func TestPartitionWipeDecision_Both(t *testing.T) {
	t.Parallel()

	changes := []clusterupdate.Change{
		{
			Field:    talosprovisioner.FieldEphemeralEncryption,
			OldValue: "luks2",
			NewValue: "none",
			Category: clusterupdate.ChangeCategoryWipeRequired,
		},
		{
			Field:    talosprovisioner.FieldStateEncryption,
			OldValue: "luks2",
			NewValue: "none",
			Category: clusterupdate.ChangeCategoryWipeRequired,
		},
	}

	ephemeral, state := talosprovisioner.PartitionWipeDecisionForTest(changes)
	assert.True(t, ephemeral, "should detect ephemeral wipe")
	assert.True(t, state, "should detect state wipe")
}

func TestPartitionWipeDecision_Empty(t *testing.T) {
	t.Parallel()

	ephemeral, state := talosprovisioner.PartitionWipeDecisionForTest(nil)
	assert.False(t, ephemeral, "should not detect ephemeral wipe from nil")
	assert.False(t, state, "should not detect state wipe from nil")
}

func TestPartitionWipeDecision_UnknownField(t *testing.T) {
	t.Parallel()

	changes := []clusterupdate.Change{
		{
			Field:    "machine.features.diskQuotaSupport",
			Category: clusterupdate.ChangeCategoryWipeRequired,
		},
	}

	ephemeral, state := talosprovisioner.PartitionWipeDecisionForTest(changes)
	assert.False(t, ephemeral, "unknown field should not trigger ephemeral wipe")
	assert.False(t, state, "unknown field should not trigger state wipe")
}
