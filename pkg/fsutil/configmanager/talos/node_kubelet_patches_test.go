package talos_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// containerMode is the Talos validation mode for nodes that run as containers,
// which is how KSail's Docker provider runs them.
type containerMode struct{}

func (containerMode) String() string        { return "container" }
func (containerMode) RequiresInstall() bool { return false }
func (containerMode) InContainer() bool     { return true }

const controlPlaneRoleLabel = "node-role.kubernetes.io/control-plane"

// nodeAndKubeletPatches are the v1alpha1 shapes KSail and its users write:
// the kubelet cert-rotation patch, a cluster-wide node label and annotation,
// and a worker-only taint plus node IP selection.
func nodeAndKubeletPatches() []talos.Patch {
	return []talos.Patch{
		{
			Path:  "kubelet-cert-rotation",
			Scope: talos.PatchScopeCluster,
			Content: []byte("machine:\n  kubelet:\n    extraArgs:\n" +
				"      rotate-server-certificates: \"true\"\n"),
		},
		{
			Path:  "node-metadata",
			Scope: talos.PatchScopeCluster,
			Content: []byte("machine:\n  nodeLabels:\n    example.test/zone: a\n" +
				"  nodeAnnotations:\n    example.test/rack: r1\n"),
		},
		{
			Path:  "worker-taint",
			Scope: talos.PatchScopeWorker,
			Content: []byte(
				"machine:\n  nodeTaints:\n    example.test/dedicated: gpu:NoSchedule\n" +
					"  kubelet:\n    nodeIP:\n      validSubnets: [10.5.0.0/24]\n",
			),
		},
	}
}

// TestNodeAndKubeletPatchesValidateUnderEveryContract renders the default
// bundle with KSail's node and kubelet patches and checks that Talos's own
// validation accepts both roles and that every setting reaches the node.
func TestNodeAndKubeletPatchesValidateUnderEveryContract(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		contract *talosconfig.VersionContract
	}{
		{name: "Talos 1.14 documents", contract: talosconfig.TalosVersion1_14},
		{name: "Talos 1.12 v1alpha1", contract: talosconfig.TalosVersion1_12},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			configs, err := talos.NewDefaultConfigsWithVersionContractAndPatches(
				"", testCase.contract, nodeAndKubeletPatches(),
			)
			require.NoError(t, err)

			// Regenerating from the stored patches migrates them again.
			configs, err = configs.WithName("renamed")
			require.NoError(t, err)

			for role, config := range map[string]talosconfig.Provider{
				"control plane": configs.ControlPlane(),
				"worker":        configs.Worker(),
			} {
				_, err := config.ValidateAsClient(containerMode{})
				require.NoError(t, err, role)

				node := config.K8sNodeConfig()
				assert.Equal(t, "a", node.Labels()["example.test/zone"], role)
				assert.Equal(t, "r1", node.Annotations()["example.test/rack"], role)
				assert.Equal(t,
					[]string{"true"},
					config.K8sKubeletConfig().ExtraArgs()["rotate-server-certificates"],
					role,
				)
			}

			controlPlane := configs.ControlPlane().K8sNodeConfig()
			assert.NotContains(t, controlPlane.Taints(), controlPlaneRoleLabel,
				"the default bundle allows scheduling on control planes")
			assert.NotContains(t, controlPlane.Taints(), "example.test/dedicated")

			worker := configs.Worker().K8sNodeConfig()
			assert.Equal(t, "gpu:NoSchedule", worker.Taints()["example.test/dedicated"])
			assert.Equal(t, []string{"10.5.0.0/24"}, worker.NodeIP().ValidSubnets())
		})
	}
}

// TestAllowSchedulingFalseKeepsTheControlPlaneTaint checks that a patch that
// turns scheduling off is honoured under the 1.14 contract.
func TestAllowSchedulingFalseKeepsTheControlPlaneTaint(t *testing.T) {
	t.Parallel()

	configs, err := talos.NewDefaultConfigsWithVersionContractAndPatches(
		"", talosconfig.TalosVersion1_14, []talos.Patch{{
			Path:    "no-control-plane-workloads",
			Scope:   talos.PatchScopeCluster,
			Content: []byte("cluster:\n  allowSchedulingOnControlPlanes: false\n"),
		}},
	)
	require.NoError(t, err)

	_, err = configs.ControlPlane().ValidateAsClient(containerMode{})
	require.NoError(t, err)
	assert.Equal(t, "NoSchedule",
		configs.ControlPlane().K8sNodeConfig().Taints()[controlPlaneRoleLabel])
}

// TestUnsupportedKubeletFieldNamesTheField checks that a kubelet field with no
// 1.14 document equivalent fails with a message the user can act on.
func TestUnsupportedKubeletFieldNamesTheField(t *testing.T) {
	t.Parallel()

	_, err := talos.NewDefaultConfigsWithVersionContractAndPatches(
		"", talosconfig.TalosVersion1_14, []talos.Patch{{
			Path:  "kubelet-mounts",
			Scope: talos.PatchScopeCluster,
			Content: []byte("machine:\n  kubelet:\n    extraMounts:\n" +
				"      - destination: /var/lib/example\n        source: /var/lib/example\n" +
				"        type: bind\n        options: [bind, rw]\n"),
		}},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "machine.kubelet.extraMounts")
	assert.Contains(t, err.Error(), "kubelet-mounts")
}
