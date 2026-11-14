package talosprovisioner_test

import (
	"testing"

	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	taloscontainer "github.com/siderolabs/talos/pkg/machinery/config/container"
	"github.com/siderolabs/talos/pkg/machinery/config/types/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// controlPlaneAt renders the control-plane config for the given contract and
// patches.
func controlPlaneAt(
	t *testing.T,
	contract *talosconfig.VersionContract,
	patches ...talosconfigmanager.Patch,
) talosconfig.Provider {
	t.Helper()

	configs, err := talosconfigmanager.NewDefaultConfigsWithVersionContractAndPatches(
		"", contract, patches,
	)
	require.NoError(t, err)

	return configs.ControlPlane()
}

// withExternalKubeNodeAnnotation adds a live-only annotation to the
// KubeNodeConfig document, as another controller would.
func withExternalKubeNodeAnnotation(
	t *testing.T,
	provider talosconfig.Provider,
) talosconfig.Provider {
	t.Helper()

	patched, err := taloscontainer.PatchDocument(
		provider,
		func(node *k8s.KubeNodeConfigV1Alpha1) error {
			if node.AnnotationsConfig == nil {
				node.AnnotationsConfig = map[string]string{}
			}

			node.AnnotationsConfig["example.com/external"] = "preserved"

			return nil
		},
	)
	require.NoError(t, err)

	return patched
}

// TestGraftNodeManagedSections_PreservesKubeNodeConfigAnnotations is the 1.14
// counterpart of the #6549 guard: the annotations live in the KubeNodeConfig
// document, so the graft must preserve live-only keys and persist the
// ownership marker there, not in the v1alpha1 machine.nodeAnnotations.
func TestGraftNodeManagedSections_PreservesKubeNodeConfigAnnotations(t *testing.T) {
	t.Parallel()

	running := withExternalKubeNodeAnnotation(t, controlPlaneAt(
		t, talosconfig.TalosVersion1_14,
		sysctlPatch("machine:\n  nodeAnnotations:\n    example.com/owned: old\n"),
	))
	desired := controlPlaneAt(
		t, talosconfig.TalosVersion1_14,
		sysctlPatch("machine:\n  nodeAnnotations:\n    example.com/owned: new\n"),
	)

	grafted, err := talosprovisioner.GraftNodeManagedSectionsForTest(desired, running)
	require.NoError(t, err)

	annotations := grafted.K8sNodeConfig().Annotations()
	assert.Equal(t, "new", annotations["example.com/owned"],
		"a KSail-owned annotation must keep its desired value")
	assert.Equal(t, "preserved", annotations["example.com/external"],
		"a live-only KubeNodeConfig annotation must survive the rebuild")
	assert.JSONEq(t, `["example.com/owned"]`, annotations[managedNodeAnnotationKey],
		"the ownership marker must be persisted in the KubeNodeConfig document")

	_, err = grafted.ValidateAsClient(nodeRuntimeMode{})
	require.NoError(t, err,
		"the graft must not reintroduce a v1alpha1 machine.nodeAnnotations next to KubeNodeConfig")

	// The next update starts from the grafted config: a key removed from the
	// patch set is removed, while the external key stays.
	next, err := talosprovisioner.GraftNodeManagedSectionsForTest(
		controlPlaneAt(t, talosconfig.TalosVersion1_14), grafted,
	)
	require.NoError(t, err)

	nextAnnotations := next.K8sNodeConfig().Annotations()
	assert.NotContains(t, nextAnnotations, "example.com/owned",
		"a previously owned key must not be reclassified as external")
	assert.Equal(t, "preserved", nextAnnotations["example.com/external"])
}

// TestGraftNodeManagedSections_CarriesLegacyAnnotationsIntoKubeNodeConfig covers
// a node whose running config predates the 1.14 contract: its live-only
// machine.nodeAnnotations keys must move into the desired KubeNodeConfig.
func TestGraftNodeManagedSections_CarriesLegacyAnnotationsIntoKubeNodeConfig(t *testing.T) {
	t.Parallel()

	running := controlPlaneAt(
		t, talosconfig.TalosVersion1_12,
		sysctlPatch("machine:\n  nodeAnnotations:\n    example.com/external: preserved\n"),
	)
	desired := controlPlaneAt(t, talosconfig.TalosVersion1_14)

	grafted, err := talosprovisioner.GraftNodeManagedSectionsForTest(desired, running)
	require.NoError(t, err)

	assert.Equal(t, "preserved",
		grafted.K8sNodeConfig().Annotations()["example.com/external"])

	_, err = grafted.ValidateAsClient(nodeRuntimeMode{})
	require.NoError(t, err)
}
