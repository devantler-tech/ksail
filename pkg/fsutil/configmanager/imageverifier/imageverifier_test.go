package imageverifier_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/imageverifier"
	"github.com/stretchr/testify/assert"
)

// TestBindirPatch_SubstitutesDistribution proves the node-image wording in the two example blocks
// takes the caller-supplied distribution name, and the shared plugin config lines are unconditional.
func TestBindirPatch_SubstitutesDistribution(t *testing.T) {
	t.Parallel()

	patch := imageverifier.BindirPatch("Kind")

	assert.Contains(t, patch, `[plugins."io.containerd.image-verifier.v1.bindir"]`)
	assert.Contains(t, patch, `bin_dir = "/opt/image-verifier/bin"`)
	assert.Contains(t, patch, "custom Kind node image")
	assert.NotContains(t, patch, "custom K3s node image")
}

// TestBindirPatch_DifferentDistributionsDiffer proves the two known callers (K3d/K3s and Kind) get
// distinct, correctly-worded output from the same shared template — not a copy-pasted constant each.
func TestBindirPatch_DifferentDistributionsDiffer(t *testing.T) {
	t.Parallel()

	k3s := imageverifier.BindirPatch("K3s")
	kind := imageverifier.BindirPatch("Kind")

	assert.NotEqual(t, k3s, kind)
	assert.Contains(t, k3s, "K3s node image")
	assert.Contains(t, kind, "Kind node image")

	// Everything BUT the distribution name is identical between the two.
	k3sNormalized := strings.ReplaceAll(k3s, "K3s", "%s")
	kindNormalized := strings.ReplaceAll(kind, "Kind", "%s")
	assert.Equal(t, k3sNormalized, kindNormalized)
}
