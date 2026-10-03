package ciharness_test

import (
	"slices"
	"testing"

	celtypes "github.com/google/cel-go/common/types"
)

// TestCELUnknownMergePreservesValues exercises the selected dependency's public
// merge behavior, including shared inputs and repeated attribute trails.
func TestCELUnknownMergePreservesValues(t *testing.T) {
	t.Parallel()

	leftTrail := celtypes.NewAttributeTrail("left")
	rightTrail := celtypes.NewAttributeTrail("right")
	left := celtypes.NewUnknown(7, leftTrail)
	right := celtypes.NewUnknown(7, rightTrail)

	if celtypes.MergeUnknowns(nil, nil) != nil ||
		celtypes.MergeUnknowns(left, nil) != left || celtypes.MergeUnknowns(nil, left) != left {
		t.Fatal("nil merge behavior changed")
	}

	merged := celtypes.MergeUnknowns(left, right)
	merged = celtypes.MergeUnknowns(merged, left)
	merged = celtypes.MergeUnknowns(merged, merged)

	merged = celtypes.MergeUnknowns(merged, celtypes.NewUnknown(3, nil))
	if !slices.Equal(merged.IDs(), []int64{3, 7}) {
		t.Fatalf("incorrect union of expression IDs: %v", merged.IDs())
	}

	trails, found := merged.GetAttributeTrails(7)
	if !found || len(trails) != 2 {
		t.Fatalf("attribute trails lost or duplicated: %v", trails)
	}

	if !trails[0].Equal(leftTrail) || !trails[1].Equal(rightTrail) {
		t.Fatalf("attribute trails changed: %v", trails)
	}
}
