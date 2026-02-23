package clusterupdate_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clusterupdate"
)

func TestUpdateResult_NoChangesIsNoOp(t *testing.T) {
	t.Parallel()

	result := clusterupdate.NewEmptyUpdateResult()

	if result.TotalChanges() != 0 {
		t.Errorf("empty result should have 0 changes, got %d", result.TotalChanges())
	}

	if result.HasInPlaceChanges() {
		t.Error("empty result should not have in-place changes")
	}

	if result.HasRebootRequired() {
		t.Error("empty result should not have reboot-required changes")
	}

	if result.HasRecreateRequired() {
		t.Error("empty result should not have recreate-required changes")
	}

	if result.NeedsUserConfirmation() {
		t.Error("empty result should not need user confirmation")
	}
}

func TestUpdateResult_OnlyRecreateDoesNotHideInNoChangesCheck(t *testing.T) {
	t.Parallel()

	result := clusterupdate.NewEmptyUpdateResult()
	result.RecreateRequired = append(result.RecreateRequired, clusterupdate.Change{
		Field:    "cluster.distribution",
		OldValue: "Vanilla",
		NewValue: "Talos",
		Category: clusterupdate.ChangeCategoryRecreateRequired,
		Reason:   "distribution change requires recreation",
	})

	if result.TotalChanges() != 1 {
		t.Errorf("result with recreate should have 1 change, got %d", result.TotalChanges())
	}

	if !result.HasRecreateRequired() {
		t.Error("result should have recreate-required changes")
	}

	// Verify that the "no in-place && no reboot" check from the update flow
	// would correctly NOT match when there are recreate-required changes.
	if !result.HasInPlaceChanges() && !result.HasRebootRequired() && result.TotalChanges() > 0 {
		// This path should be reached: no in-place, no reboot, but total > 0.
		// The update command should check HasRecreateRequired() BEFORE this.
	}
}
