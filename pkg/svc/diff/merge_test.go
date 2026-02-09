package diff_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/diff"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/types"
)

//nolint:funlen // Table-driven test with multiple sub-tests is clearer as single function
func TestMergeProvisionerDiff(t *testing.T) {
	t.Parallel()

	t.Run("nil provisioner diff is no-op", func(t *testing.T) {
		t.Parallel()

		main := &types.UpdateResult{
			InPlaceChanges: []types.Change{
				{Field: "cluster.cni", Category: types.ChangeCategoryInPlace},
			},
			RebootRequired:   []types.Change{},
			RecreateRequired: []types.Change{},
		}

		diff.MergeProvisionerDiff(main, nil)

		if len(main.InPlaceChanges) != 1 {
			t.Errorf("expected 1 in-place change, got %d", len(main.InPlaceChanges))
		}
	})

	t.Run("adds unique provisioner changes", func(t *testing.T) {
		t.Parallel()

		main := &types.UpdateResult{
			InPlaceChanges:   []types.Change{{Field: "cluster.cni"}},
			RebootRequired:   []types.Change{},
			RecreateRequired: []types.Change{},
		}

		provisioner := &types.UpdateResult{
			InPlaceChanges:   []types.Change{{Field: "talos.workers"}},
			RebootRequired:   []types.Change{{Field: "machine.install"}},
			RecreateRequired: []types.Change{},
		}

		diff.MergeProvisionerDiff(main, provisioner)

		if len(main.InPlaceChanges) != 2 {
			t.Errorf("expected 2 in-place changes, got %d", len(main.InPlaceChanges))
		}

		if len(main.RebootRequired) != 1 {
			t.Errorf("expected 1 reboot-required change, got %d", len(main.RebootRequired))
		}
	})

	t.Run("deduplicates existing fields", func(t *testing.T) {
		t.Parallel()

		main := &types.UpdateResult{
			InPlaceChanges:   []types.Change{{Field: "cluster.cni"}},
			RebootRequired:   []types.Change{},
			RecreateRequired: []types.Change{{Field: "cluster.distribution"}},
		}

		provisioner := &types.UpdateResult{
			InPlaceChanges:   []types.Change{{Field: "cluster.cni"}}, // duplicate
			RebootRequired:   []types.Change{},
			RecreateRequired: []types.Change{{Field: "cluster.distribution"}}, // duplicate
		}

		diff.MergeProvisionerDiff(main, provisioner)

		if len(main.InPlaceChanges) != 1 {
			t.Errorf("expected 1 in-place change (deduplicated), got %d", len(main.InPlaceChanges))
		}

		if len(main.RecreateRequired) != 1 {
			t.Errorf(
				"expected 1 recreate change (deduplicated), got %d",
				len(main.RecreateRequired),
			)
		}
	})

	t.Run("deduplicates fields with cluster prefix mismatch", func(t *testing.T) {
		t.Parallel()

		main := &types.UpdateResult{
			InPlaceChanges:   []types.Change{},
			RebootRequired:   []types.Change{},
			RecreateRequired: []types.Change{{Field: "cluster.vanilla.mirrorsDir"}},
		}

		provisioner := &types.UpdateResult{
			InPlaceChanges: []types.Change{},
			RebootRequired: []types.Change{},
			RecreateRequired: []types.Change{
				{Field: "vanilla.mirrorsDir"},
			}, // same field, no prefix
		}

		diff.MergeProvisionerDiff(main, provisioner)

		if len(main.RecreateRequired) != 1 {
			t.Errorf(
				"expected 1 recreate change (deduplicated across prefix), got %d",
				len(main.RecreateRequired),
			)
		}
	})
}
