package talosprovisioner_test

import (
	"testing"

	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/types/k8s"
	talosruntime "github.com/siderolabs/talos/pkg/machinery/config/types/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

// metalClientMode is the runtime mode Talos validates a cloud server's
// configuration under: installed to disk, not in a container.
type metalClientMode struct{}

func (metalClientMode) String() string        { return "metal" }
func (metalClientMode) RequiresInstall() bool { return true }
func (metalClientMode) InContainer() bool     { return false }

const storageWorkerPatch = `machine:
  nodeLabels:
    node.longhorn.io/create-default-disk: "true"
    example.com/kept: "yes"
  kubelet:
    extraArgs:
      node-labels: node.longhorn.io/create-default-disk=true,example.com/role=storage
`

func autoscalerWorkerAt(
	t *testing.T,
	contract *talosconfig.VersionContract,
) talosconfig.Provider {
	t.Helper()

	configs, err := talosconfigmanager.NewDefaultConfigsWithVersionContractAndPatches(
		"", contract, []talosconfigmanager.Patch{sysctlPatch(storageWorkerPatch)},
	)
	require.NoError(t, err)

	cfgBytes, err := talosprovisioner.GenerateAutoscalerWorkerConfig(
		configs.Worker(),
		map[string]string{"workload": "gpu"},
		[]corev1.Taint{
			{Key: "dedicated", Value: "gpu", Effect: corev1.TaintEffectNoSchedule},
			{Key: "spot", Effect: corev1.TaintEffectNoExecute},
		},
	)
	require.NoError(t, err)

	loaded, err := configloader.NewFromBytes(cfgBytes)
	require.NoError(t, err)

	return loaded
}

func kubeletConfigDocument(provider talosconfig.Provider) *k8s.KubeletConfigV1Alpha1 {
	for _, document := range provider.Documents() {
		if kubelet, ok := document.(*k8s.KubeletConfigV1Alpha1); ok {
			return kubelet
		}
	}

	return nil
}

// TestGenerateAutoscalerWorkerConfig_KubeNodeConfigLabelsAndTaints guards #8366:
// under the Talos 1.14 contract the pool labels, the autoscaled marker and the
// pool taints belong in the KubeNodeConfig document, because Talos refuses a
// v1alpha1 machine.nodeLabels or machine.nodeTaints next to it. The install wipe
// likewise belongs in the UnattendedInstallConfig document.
func TestGenerateAutoscalerWorkerConfig_KubeNodeConfigLabelsAndTaints(t *testing.T) {
	t.Parallel()

	worker := autoscalerWorkerAt(t, talosconfig.TalosVersion1_14)

	var nodeConfig *k8s.KubeNodeConfigV1Alpha1

	for _, document := range worker.Documents() {
		if node, ok := document.(*k8s.KubeNodeConfigV1Alpha1); ok {
			nodeConfig = node
		}
	}

	require.NotNil(t, nodeConfig, "a Talos 1.14 worker carries a KubeNodeConfig document")

	labels := nodeConfig.Labels()
	assert.Equal(t, "gpu", labels["workload"])
	assert.Equal(t, "true", labels[talosprovisioner.LabelAutoscaled])
	assert.Equal(t, "yes", labels["example.com/kept"])
	assert.NotContains(t, labels, "node.longhorn.io/create-default-disk")

	taints := nodeConfig.Taints()
	assert.Equal(t, "gpu:NoSchedule", taints["dedicated"])
	assert.Equal(t, ":NoExecute", taints["spot"])

	raw := worker.RawV1Alpha1()
	require.NotNil(t, raw)
	require.NotNil(t, raw.MachineConfig)
	//nolint:staticcheck // asserting the legacy fields stay unset under the 1.14 contract
	assert.Nil(t, raw.MachineConfig.MachineNodeLabels)
	//nolint:staticcheck // asserting the legacy fields stay unset under the 1.14 contract
	assert.Nil(t, raw.MachineConfig.MachineNodeTaints)

	//nolint:staticcheck // asserting the legacy install field stays unset under the 1.14 contract
	assert.Nil(t, raw.MachineConfig.MachineInstall)

	var install *talosruntime.UnattendedInstallConfigV1Alpha1

	for _, document := range worker.Documents() {
		if candidate, ok := document.(*talosruntime.UnattendedInstallConfigV1Alpha1); ok {
			install = candidate
		}
	}

	require.NotNil(t, install, "a Talos 1.14 worker carries an UnattendedInstallConfig document")
	assert.True(t, install.VolumeWipe(), "autoscaler nodes wipe the install disk")

	kubelet := kubeletConfigDocument(worker)
	require.NotNil(t, kubelet, "a Talos 1.14 worker carries a KubeletConfig document")
	assert.Equal(t,
		[]string{"example.com/role=storage"},
		kubelet.KubeletArgs.ToMap()["node-labels"],
		"the Longhorn registration label is stripped from the KubeletConfig arguments",
	)

	_, err := worker.ValidateAsClient(metalClientMode{})
	require.NoError(t, err, "Talos must accept the autoscaler worker configuration")
}

// TestGenerateAutoscalerWorkerConfig_LegacyContractKeepsV1Alpha1Labels pins the
// pre-1.14 shape: no KubeNodeConfig document, labels and taints in v1alpha1.
func TestGenerateAutoscalerWorkerConfig_LegacyContractKeepsV1Alpha1Labels(t *testing.T) {
	t.Parallel()

	worker := autoscalerWorkerAt(t, talosconfig.TalosVersion1_13)

	for _, document := range worker.Documents() {
		_, isNodeConfig := document.(*k8s.KubeNodeConfigV1Alpha1)
		assert.False(t, isNodeConfig, "a Talos 1.13 worker has no KubeNodeConfig document")
	}

	raw := worker.RawV1Alpha1()
	require.NotNil(t, raw)
	//nolint:staticcheck // the legacy fields are the 1.13 contract's node labels and taints
	labels := raw.MachineConfig.MachineNodeLabels
	assert.Equal(t, "gpu", labels["workload"])
	assert.Equal(t, "true", labels[talosprovisioner.LabelAutoscaled])
	assert.NotContains(t, labels, "node.longhorn.io/create-default-disk")
	//nolint:staticcheck // the legacy fields are the 1.13 contract's node labels and taints
	assert.Equal(t, "gpu:NoSchedule", raw.MachineConfig.MachineNodeTaints["dedicated"])

	//nolint:staticcheck // the legacy kubelet arguments of the 1.13 contract
	assert.Equal(t,
		[]string{"example.com/role=storage"},
		raw.MachineConfig.MachineKubelet.KubeletExtraArgs.ToMap()["node-labels"],
	)

	_, err := worker.ValidateAsClient(metalClientMode{})
	require.NoError(t, err)
}
