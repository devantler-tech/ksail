package talosprovisioner_test

import (
	"testing"

	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
)

func TestAvailableNodeIndices_EmptyList(t *testing.T) {
	t.Parallel()

	result := talosprovisioner.AvailableNodeIndicesForTest(nil, "mycluster-controlplane-", 1)
	assert.Equal(t, []int{1}, result)
}

func TestAvailableNodeIndices_EmptyList_MultipleNodes(t *testing.T) {
	t.Parallel()

	result := talosprovisioner.AvailableNodeIndicesForTest(nil, "mycluster-controlplane-", 3)
	assert.Equal(t, []int{1, 2, 3}, result)
}

func TestAvailableNodeIndices_NoMatchingPrefix(t *testing.T) {
	t.Parallel()

	names := []string{"other-controlplane-1", "other-worker-1"}
	result := talosprovisioner.AvailableNodeIndicesForTest(names, "mycluster-controlplane-", 1)
	assert.Equal(t, []int{1}, result)
}

func TestAvailableNodeIndices_SingleNode(t *testing.T) {
	t.Parallel()

	names := []string{"mycluster-controlplane-1"}
	result := talosprovisioner.AvailableNodeIndicesForTest(names, "mycluster-controlplane-", 1)
	assert.Equal(t, []int{2}, result)
}

func TestAvailableNodeIndices_Contiguous_ExtendsPastMax(t *testing.T) {
	t.Parallel()

	names := []string{
		"mycluster-controlplane-1",
		"mycluster-controlplane-2",
		"mycluster-controlplane-3",
	}
	result := talosprovisioner.AvailableNodeIndicesForTest(names, "mycluster-controlplane-", 1)
	assert.Equal(t, []int{4}, result)
}

func TestAvailableNodeIndices_MixedRoles(t *testing.T) {
	t.Parallel()

	names := []string{
		"mycluster-controlplane-1",
		"mycluster-worker-1",
		"mycluster-controlplane-2",
	}
	result := talosprovisioner.AvailableNodeIndicesForTest(names, "mycluster-controlplane-", 1)
	assert.Equal(t, []int{3}, result)
}

// TestAvailableNodeIndices_FillsLowestGap is the core behaviour from #5312: with a
// freed middle index, the next node reclaims that gap instead of allocating max+1.
func TestAvailableNodeIndices_FillsLowestGap(t *testing.T) {
	t.Parallel()

	names := []string{
		"mycluster-controlplane-1",
		"mycluster-controlplane-3",
	}
	result := talosprovisioner.AvailableNodeIndicesForTest(names, "mycluster-controlplane-", 1)
	assert.Equal(t, []int{2}, result)
}

// TestAvailableNodeIndices_ProdRecoveryScenario reproduces the exact issue report:
// after losing prod-control-plane-2, the restore must recreate -2, not -4.
func TestAvailableNodeIndices_ProdRecoveryScenario(t *testing.T) {
	t.Parallel()

	names := []string{"prod-control-plane-1", "prod-control-plane-3"}
	result := talosprovisioner.AvailableNodeIndicesForTest(names, "prod-control-plane-", 1)
	assert.Equal(t, []int{2}, result)
}

// TestAvailableNodeIndices_FillsGapThenExtends covers a multi-node scale-up that
// spans a gap: index 2 is reclaimed first, then the series continues past the max.
func TestAvailableNodeIndices_FillsGapThenExtends(t *testing.T) {
	t.Parallel()

	names := []string{
		"mycluster-worker-1",
		"mycluster-worker-3",
	}
	result := talosprovisioner.AvailableNodeIndicesForTest(names, "mycluster-worker-", 2)
	assert.Equal(t, []int{2, 4}, result)
}

// TestAvailableNodeIndices_LowestOfMultipleGaps picks the lowest gap even when a
// larger one exists higher in the series.
func TestAvailableNodeIndices_LowestOfMultipleGaps(t *testing.T) {
	t.Parallel()

	names := []string{
		"mycluster-worker-1",
		"mycluster-worker-5",
	}
	result := talosprovisioner.AvailableNodeIndicesForTest(names, "mycluster-worker-", 1)
	assert.Equal(t, []int{2}, result)
}

// TestAvailableNodeIndices_ReclaimsLeadingGap reclaims index 1 when the lowest
// node was the one removed.
func TestAvailableNodeIndices_ReclaimsLeadingGap(t *testing.T) {
	t.Parallel()

	names := []string{
		"mycluster-worker-2",
		"mycluster-worker-4",
	}
	result := talosprovisioner.AvailableNodeIndicesForTest(names, "mycluster-worker-", 3)
	assert.Equal(t, []int{1, 3, 5}, result)
}

func TestAvailableNodeIndices_NonNumericSuffix(t *testing.T) {
	t.Parallel()

	// Names whose suffix after the prefix is not a positive integer are ignored.
	names := []string{
		"mycluster-worker-abc",
		"mycluster-worker-1",
	}
	result := talosprovisioner.AvailableNodeIndicesForTest(names, "mycluster-worker-", 1)
	assert.Equal(t, []int{2}, result)
}

func TestAvailableNodeIndices_ZeroCount(t *testing.T) {
	t.Parallel()

	names := []string{"mycluster-worker-1"}
	result := talosprovisioner.AvailableNodeIndicesForTest(names, "mycluster-worker-", 0)
	assert.Empty(t, result)
}
