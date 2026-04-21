package talosprovisioner_test

import (
	"testing"

	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
)

func TestNextNodeIndexFromNames_EmptyList(t *testing.T) {
	t.Parallel()

	result := talosprovisioner.NextNodeIndexFromNamesForTest(nil, "mycluster-controlplane-")
	assert.Equal(t, 1, result)
}

func TestNextNodeIndexFromNames_NoMatchingPrefix(t *testing.T) {
	t.Parallel()

	names := []string{"other-controlplane-1", "other-worker-1"}
	result := talosprovisioner.NextNodeIndexFromNamesForTest(names, "mycluster-controlplane-")
	assert.Equal(t, 1, result)
}

func TestNextNodeIndexFromNames_SingleNode(t *testing.T) {
	t.Parallel()

	names := []string{"mycluster-controlplane-1"}
	result := talosprovisioner.NextNodeIndexFromNamesForTest(names, "mycluster-controlplane-")
	assert.Equal(t, 2, result)
}

func TestNextNodeIndexFromNames_MultipleNodes(t *testing.T) {
	t.Parallel()

	names := []string{
		"mycluster-controlplane-1",
		"mycluster-controlplane-2",
		"mycluster-controlplane-3",
	}
	result := talosprovisioner.NextNodeIndexFromNamesForTest(names, "mycluster-controlplane-")
	assert.Equal(t, 4, result)
}

func TestNextNodeIndexFromNames_MixedRoles(t *testing.T) {
	t.Parallel()

	names := []string{
		"mycluster-controlplane-1",
		"mycluster-worker-1",
		"mycluster-controlplane-2",
	}
	result := talosprovisioner.NextNodeIndexFromNamesForTest(names, "mycluster-controlplane-")
	assert.Equal(t, 3, result)
}

func TestNextNodeIndexFromNames_GapInIndexes(t *testing.T) {
	t.Parallel()

	// Gaps should not affect the result — we always track the maximum seen index.
	names := []string{
		"mycluster-worker-1",
		"mycluster-worker-5",
	}
	result := talosprovisioner.NextNodeIndexFromNamesForTest(names, "mycluster-worker-")
	assert.Equal(t, 6, result)
}

func TestNextNodeIndexFromNames_NonNumericSuffix(t *testing.T) {
	t.Parallel()

	// Names whose suffix after the prefix is not an integer must be ignored.
	names := []string{
		"mycluster-worker-abc",
		"mycluster-worker-1",
	}
	result := talosprovisioner.NextNodeIndexFromNamesForTest(names, "mycluster-worker-")
	assert.Equal(t, 2, result)
}
