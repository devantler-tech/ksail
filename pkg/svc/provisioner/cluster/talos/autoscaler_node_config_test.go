package talosprovisioner_test

import (
	"context"
	"testing"

	ksailv1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

const (
	autoscalerTestPool   = "autoscale-cx43"
	longhornDefaultDisk  = "node.longhorn.io/create-default-disk"
	autoscalerTestNodeIP = "10.0.0.5"
)

// autoscalerNodeFixture returns a provisioner whose static workers carry the Longhorn
// default-disk label, and the running config of an autoscaler node in the test pool:
// the autoscaler worker config it booted from.
func autoscalerNodeFixture(
	t *testing.T,
) (*talosprovisioner.Provisioner, talosconfig.Provider, talosconfig.Provider) {
	t.Helper()

	configs, err := talosconfigmanager.NewDefaultConfigsWithPatches([]talosconfigmanager.Patch{{
		Path:    "workers/longhorn.yaml",
		Scope:   talosconfigmanager.PatchScopeWorker,
		Content: []byte("machine:\n  nodeLabels:\n    " + longhornDefaultDisk + ": \"true\"\n"),
	}})
	require.NoError(t, err)

	pool := ksailv1alpha1.NodePool{
		Name:   autoscalerTestPool,
		Labels: map[string]string{"workload": "batch"},
		Taints: []ksailv1alpha1.NodePoolTaint{
			{Key: "dedicated", Value: "batch", Effect: ksailv1alpha1.TaintEffectNoSchedule},
		},
	}

	bootConfig, err := talosprovisioner.GenerateAutoscalerWorkerConfig(
		configs.Worker(), pool.Labels,
		[]corev1.Taint{{Key: "dedicated", Value: "batch", Effect: corev1.TaintEffectNoSchedule}},
	)
	require.NoError(t, err)

	running, err := configloader.NewFromBytes(bootConfig)
	require.NoError(t, err)

	prov := talosprovisioner.NewProvisioner(configs, nil).
		WithHetznerOptions(ksailv1alpha1.OptionsHetzner{
			NodeAutoscalerEnabled:   true,
			AutoscalerNodePoolNames: []string{pool.Name},
			AutoscalerNodePools:     []ksailv1alpha1.NodePool{pool},
		}).
		WithNodeConfigFetcherForTest(func(context.Context, string) (talosconfig.Provider, error) {
			return running, nil
		})

	return prov, running, parseConfig(t, configs.ControlPlane())
}

func autoscalerServer(pool string) *hcloud.Server {
	return &hcloud.Server{
		Name:   "autoscaler-node",
		Labels: map[string]string{hetzner.LabelAutoscalerNodeGroup: pool},
	}
}

// TestAutoscalerNodeDesiredConfigKeepsAutoscalerShape pins #7013: an update rebuilds the
// config of a node the cluster autoscaler provisioned before applying or staging it,
// and that rebuild must keep the shape the node booted from. The static worker config
// would drop the ksail.io/autoscaled marker and the pool labels and taints, and add
// the Longhorn default-disk label.
func TestAutoscalerNodeDesiredConfigKeepsAutoscalerShape(t *testing.T) {
	t.Parallel()

	prov, running, secretsSource := autoscalerNodeFixture(t)

	node, err := prov.AutoscalerNodeForTest(
		autoscalerServer(autoscalerTestPool), autoscalerTestNodeIP,
	)
	require.NoError(t, err)

	desired, err := prov.FetchAndBuildDesiredNodeConfigForTest(t.Context(), node, secretsSource)
	require.NoError(t, err)

	machine := desired.RawV1Alpha1().MachineConfig
	assert.Equal(t, "true", machine.MachineNodeLabels[talosprovisioner.LabelAutoscaled])
	assert.Equal(t, "batch", machine.MachineNodeLabels["workload"])
	assert.Equal(t, "batch:NoSchedule", machine.MachineNodeTaints["dedicated"])
	assert.NotContains(t, machine.MachineNodeLabels, longhornDefaultDisk)

	diff, err := talosprovisioner.MachineConfigDiffForTest(running, desired)
	require.NoError(t, err)
	assert.Empty(t, diff, "an autoscaler node on its pool's config must not be rewritten")
}

// TestStaticWorkerDesiredConfigKeepsStaticShape is the control: a KSail-owned worker
// still gets the static worker config, Longhorn label included and no autoscaler marker.
func TestStaticWorkerDesiredConfigKeepsStaticShape(t *testing.T) {
	t.Parallel()

	prov, _, secretsSource := autoscalerNodeFixture(t)

	desired, err := prov.FetchAndBuildDesiredNodeConfigForTest(
		t.Context(),
		talosprovisioner.NodeWithRoleForTest{
			IP:   autoscalerTestNodeIP,
			Role: talosprovisioner.RoleWorker,
		},
		secretsSource,
	)
	require.NoError(t, err)

	labels := desired.RawV1Alpha1().MachineConfig.MachineNodeLabels
	assert.Equal(t, "true", labels[longhornDefaultDisk])
	assert.NotContains(t, labels, talosprovisioner.LabelAutoscaled)
}

// TestAutoscalerNodeOfUnconfiguredPoolIsRefused pins that a server whose pool is not in
// spec.cluster.autoscaler.node.pools is refused before any config is built for it,
// because the shape it booted from is unknown.
func TestAutoscalerNodeOfUnconfiguredPoolIsRefused(t *testing.T) {
	t.Parallel()

	prov, _, _ := autoscalerNodeFixture(t)

	_, err := prov.AutoscalerNodeForTest(autoscalerServer("removed-pool"), autoscalerTestNodeIP)

	require.ErrorIs(t, err, talosprovisioner.ErrUnknownAutoscalerPoolForTest)
	assert.ErrorContains(t, err, `"removed-pool"`)
}
