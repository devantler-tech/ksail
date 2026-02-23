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

func TestUpdateResult_RecreateChangesAreDetected(t *testing.T) {
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

	// Recreate-required changes are not reflected in HasInPlaceChanges
	// or HasRebootRequired, but TotalChanges must still count them.
	if result.HasInPlaceChanges() {
		t.Error("result should not have in-place changes")
	}

	if result.HasRebootRequired() {
		t.Error("result should not have reboot-required changes")
	}
}
