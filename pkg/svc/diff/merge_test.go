package diff_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/diff"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clusterupdate"
)

//nolint:funlen // Table-driven test with multiple sub-tests is clearer as single function
func TestMergeProvisionerDiff(t *testing.T) {
	t.Parallel()

	t.Run("nil provisioner diff is no-op", func(t *testing.T) {
		t.Parallel()

		main := &clusterupdate.UpdateResult{
			InPlaceChanges: []clusterupdate.Change{
				{Field: "cluster.cni", Category: clusterupdate.ChangeCategoryInPlace},
			},
			RebootRequired:   []clusterupdate.Change{},
			RecreateRequired: []clusterupdate.Change{},
		}

		diff.MergeProvisionerDiff(main, nil)

		if len(main.InPlaceChanges) != 1 {
			t.Errorf("expected 1 in-place change, got %d", len(main.InPlaceChanges))
		}
	})

	t.Run("adds unique provisioner changes", func(t *testing.T) {
		t.Parallel()

		main := &clusterupdate.UpdateResult{
			InPlaceChanges:   []clusterupdate.Change{{Field: "cluster.cni"}},
			RebootRequired:   []clusterupdate.Change{},
			RecreateRequired: []clusterupdate.Change{},
		}

		provisioner := &clusterupdate.UpdateResult{
			InPlaceChanges:   []clusterupdate.Change{{Field: "talos.workers"}},
			RebootRequired:   []clusterupdate.Change{{Field: "machine.install"}},
			RecreateRequired: []clusterupdate.Change{},
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

		main := &clusterupdate.UpdateResult{
			InPlaceChanges:   []clusterupdate.Change{{Field: "cluster.cni"}},
			RebootRequired:   []clusterupdate.Change{},
			RecreateRequired: []clusterupdate.Change{{Field: "cluster.distribution"}},
		}

		provisioner := &clusterupdate.UpdateResult{
			InPlaceChanges:   []clusterupdate.Change{{Field: "cluster.cni"}}, // duplicate
			RebootRequired:   []clusterupdate.Change{},
			RecreateRequired: []clusterupdate.Change{{Field: "cluster.distribution"}}, // duplicate
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

		main := &clusterupdate.UpdateResult{
			InPlaceChanges:   []clusterupdate.Change{},
			RebootRequired:   []clusterupdate.Change{},
			RecreateRequired: []clusterupdate.Change{{Field: "cluster.vanilla.mirrorsDir"}},
		}

		provisioner := &clusterupdate.UpdateResult{
			InPlaceChanges: []clusterupdate.Change{},
			RebootRequired: []clusterupdate.Change{},
			RecreateRequired: []clusterupdate.Change{
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
