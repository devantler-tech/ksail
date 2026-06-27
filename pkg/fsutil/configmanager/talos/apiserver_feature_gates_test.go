package talos_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIServerFeatureGatesPatch(t *testing.T) {
	t.Parallel()

	patch := talos.APIServerFeatureGatesPatch()

	expectedContent := "cluster:\n" +
		"  apiServer:\n" +
		"    extraArgs:\n" +
		"      feature-gates: MutatingAdmissionPolicy=true\n" +
		"      runtime-config: admissionregistration.k8s.io/v1beta1=true\n"

	assert.Equal(t, "ksail-apiserver-feature-gates", patch.Path)
	assert.Equal(t, talos.PatchScopeCluster, patch.Scope)
	assert.Equal(t, []byte(expectedContent), patch.Content)
}

// TestAPIServerFeatureGatesPatch_AppliesToTalosConfig verifies the patch is a
// well-formed Talos strategic merge patch that generates and merges cleanly,
// setting the intended kube-apiserver extraArgs on the rendered control-plane
// config. The test above only asserts the raw bytes, so it would not catch a
// YAML typo, a wrong key, or a Talos schema change that breaks patch
// application — the failure mode that actually matters, since the patch is run
// through configpatcher.LoadPatch during config generation.
func TestAPIServerFeatureGatesPatch_AppliesToTalosConfig(t *testing.T) {
	t.Parallel()

	configs, err := talos.NewDefaultConfigsWithPatches(
		[]talos.Patch{talos.APIServerFeatureGatesPatch()},
	)
	require.NoError(t, err)

	controlPlane := configs.ControlPlane()
	require.NotNil(t, controlPlane)

	extraArgs := controlPlane.Cluster().APIServer().ExtraArgs()

	// Assert the full map so the patch is verified to set exactly these two
	// apiserver args and nothing else — a whole-map comparison catches both a
	// wrong/missing value and any unexpected extra key the patch might leak.
	assert.Equal(t, map[string][]string{
		"feature-gates":  {"MutatingAdmissionPolicy=true"},
		"runtime-config": {"admissionregistration.k8s.io/v1beta1=true"},
	}, extraArgs)
}
