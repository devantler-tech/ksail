package talos_test

import (
	"testing"

	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
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

func TestAPIServerFeatureGatesPatch_AppliesToTalos14Config(t *testing.T) {
	t.Parallel()

	manager := talos.NewConfigManager(t.TempDir(), "talos-114", "1.36.0", "10.5.0.0/24").
		WithVersionContract(talosconfig.TalosVersion1_14).
		WithAdditionalPatches([]talos.Patch{talos.APIServerFeatureGatesPatch()})

	configs, err := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err)

	assert.Equal(t, map[string][]string{
		"feature-gates":  {"MutatingAdmissionPolicy=true"},
		"runtime-config": {"admissionregistration.k8s.io/v1beta1=true"},
	}, configs.ControlPlane().K8sAPIServerConfig().ExtraArgs())
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

	extraArgs := controlPlane.K8sAPIServerConfig().ExtraArgs()

	// Assert the full map so the patch is verified to set exactly these two
	// apiserver args and nothing else — a whole-map comparison catches both a
	// wrong/missing value and any unexpected extra key the patch might leak.
	assert.Equal(t, map[string][]string{
		"feature-gates":  {"MutatingAdmissionPolicy=true"},
		"runtime-config": {"admissionregistration.k8s.io/v1beta1=true"},
	}, extraArgs)
}
