package talosprovisioner_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const longhornDefaultDiskLabel = "node.longhorn.io/create-default-disk"

// workerConfigsWithLonghornLabel loads real KSail Talos configs whose worker patch sets the
// Longhorn default-disk label, the way a Hetzner cluster with Longhorn storage does.
func workerConfigsWithLonghornLabel(t *testing.T) *talosconfigmanager.Configs {
	t.Helper()

	patchesDir := t.TempDir()
	workersDir := filepath.Join(patchesDir, talosconfigmanager.PatchSubdirWorkers)
	require.NoError(t, os.MkdirAll(workersDir, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(workersDir, "longhorn.yaml"),
		[]byte("machine:\n  nodeLabels:\n    "+longhornDefaultDiskLabel+": \"true\"\n"),
		0o600,
	))

	configs, err := talosconfigmanager.
		NewConfigManager(patchesDir, "autoscaler-payload", "1.32.0", "10.5.0.0/24").
		Load(configmanager.LoadOptions{})
	require.NoError(t, err)

	return configs
}

// The config a new autoscaler node boots from is the pool's cloudInit inside the Secret, not the
// intermediate generated worker config. This builds that Secret the way cluster create and update
// do, decodes each pool's cloudInit the way the autoscaler and Talos do, and checks the booted
// config carries the autoscaler marker and not the Longhorn default-disk label (#7013).
func TestAutoscalerSecretPayloadBootsEveryPoolWithTheAutoscalerShape(t *testing.T) {
	t.Parallel()

	configs := workerConfigsWithLonghornLabel(t)
	require.Equal(
		t,
		"true",
		configs.Bundle().Worker().RawV1Alpha1().MachineConfig.MachineNodeLabels[longhornDefaultDiskLabel],
		"precondition: the static worker config carries the Longhorn label",
	)

	provisioner := talosprovisioner.NewProvisioner(nil, nil).
		WithHetznerOptions(v1alpha1.OptionsHetzner{
			NodeAutoscalerEnabled: true,
			AutoscalerNodePools: []v1alpha1.NodePool{
				{Name: "autoscale-cx43", Labels: map[string]string{"workload": "general"}},
				{Name: "autoscale-cx53"},
			},
		})

	pools, err := provisioner.BuildAutoscalerPoolConfigsForTest(configs.Bundle())
	require.NoError(t, err)
	require.Len(t, pools, 2)

	clientset := fake.NewClientset()
	_, err = talosprovisioner.ApplyAutoscalerConfigSecret(
		context.Background(),
		clientset,
		"123",
		pools,
	)
	require.NoError(t, err)

	secret, err := clientset.CoreV1().Secrets("kube-system").Get(
		context.Background(), "cluster-autoscaler-config", metav1.GetOptions{},
	)
	require.NoError(t, err)

	payload := decodeClusterConfig(t, secret.Data[clusterConfigSecretKey])
	require.Len(t, payload.NodeConfigs, 2)

	for _, name := range []string{"autoscale-cx43", "autoscale-cx53"} {
		nodeConfig, ok := payload.NodeConfigs[name]
		require.True(t, ok, "pool %s is missing from the Secret", name)

		booted, err := configloader.NewFromBytes(decodePoolCloudInit(t, nodeConfig.CloudInit))
		require.NoError(t, err, "pool %s cloudInit is not a Talos config", name)

		labels := booted.RawV1Alpha1().MachineConfig.MachineNodeLabels
		assert.Equal(t, "true", labels[talosprovisioner.LabelAutoscaled],
			"pool %s would boot without the autoscaler marker", name)
		assert.NotContains(t, labels, longhornDefaultDiskLabel,
			"pool %s would boot with the Longhorn default-disk label", name)
	}

	cx43 := payload.NodeConfigs["autoscale-cx43"]
	booted, err := configloader.NewFromBytes(decodePoolCloudInit(t, cx43.CloudInit))
	require.NoError(t, err)
	assert.Equal(t, "general", booted.RawV1Alpha1().MachineConfig.MachineNodeLabels["workload"],
		"a pool's own labels reach the config its nodes boot from")
}
