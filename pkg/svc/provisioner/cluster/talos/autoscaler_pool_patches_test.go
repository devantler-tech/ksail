package talosprovisioner_test

import (
	"context"
	"testing"

	ksailv1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/types/block"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutoscalerPoolPatchesFollowWorkersFromBootstrapToReconcile(t *testing.T) {
	t.Parallel()

	sharedPatch := poolRawVolumePatch("scratch", "8GiB") + "---\n" +
		poolRawVolumePatch("retained", "4GiB") + "---\n" +
		poolEphemeralMaxPatch("-104GiB")
	configs, err := talosconfigmanager.NewDefaultConfigsWithPatches([]talosconfigmanager.Patch{{
		Path:    "workers/storage.yaml",
		Scope:   talosconfigmanager.PatchScopeWorker,
		Content: []byte(sharedPatch),
	}})
	require.NoError(t, err)

	lastPatch := poolRawVolumePatch("scratch", "16GiB") + "---\n" +
		poolRawVolumePatch("additional", "32GiB") + "---\n" +
		poolEphemeralMaxPatch("44GiB")
	pool := ksailv1alpha1.NodePool{
		Name:         autoscalerTestPool,
		TalosPatches: []string{poolRawVolumePatch("scratch", "12GiB"), lastPatch},
	}
	prov := talosprovisioner.NewProvisioner(configs, nil).
		WithHetznerOptions(ksailv1alpha1.OptionsHetzner{
			NodeAutoscalerEnabled: true,
			AutoscalerNodePools:   []ksailv1alpha1.NodePool{pool, {Name: "other-pool"}},
		}).
		WithNodeConfigFetcherForTest(func(context.Context, string) (talosconfig.Provider, error) {
			return configs.Worker(), nil
		})

	_, err = prov.DiffConfig(t.Context(), "", nil, nil)
	require.NoError(t, err)

	templates, err := prov.BuildAutoscalerPoolConfigsForTest(configs.Bundle())
	require.NoError(t, err)
	require.Len(t, templates, 2)

	bootstrap, err := configloader.NewFromBytes(templates[0].WorkerConfigYAML)
	require.NoError(t, err)
	otherPool, err := configloader.NewFromBytes(templates[1].WorkerConfigYAML)
	require.NoError(t, err)

	sharedLayout := map[string]block.Size{
		"scratch":  block.MustSize("8GiB"),
		"retained": block.MustSize("4GiB"),
	}
	poolLayout := map[string]block.Size{
		"scratch":    block.MustSize("16GiB"),
		"retained":   block.MustSize("4GiB"),
		"additional": block.MustSize("32GiB"),
	}
	assertPoolLayout(t, bootstrap, poolLayout, "44GiB")
	assertPoolLayout(t, otherPool, sharedLayout, "-104GiB")
	assertPoolLayout(t, configs.Worker(), sharedLayout, "-104GiB")

	node, err := prov.AutoscalerNodeForTest(autoscalerServer(pool.Name), autoscalerTestNodeIP)
	require.NoError(t, err)
	reconciled, err := prov.FetchAndBuildDesiredNodeConfigForTest(
		t.Context(), node, configs.ControlPlane(),
	)
	require.NoError(t, err)
	assertPoolLayout(t, reconciled, poolLayout, "44GiB")
}

func poolEphemeralMaxPatch(size string) string {
	return "apiVersion: v1alpha1\nkind: VolumeConfig\nname: EPHEMERAL\n" +
		"provisioning:\n  diskSelector:\n    match: system_disk\n  minSize: 40GiB\n  maxSize: " + size + "\n"
}

func assertPoolLayout(
	t *testing.T,
	cfg talosconfig.Provider,
	rawLayout map[string]block.Size,
	maxEphemeral string,
) {
	t.Helper()
	assert.Equal(t, rawLayout, rawVolumeSizes(cfg))

	for _, document := range cfg.Documents() {
		volume, ok := document.(*block.VolumeConfigV1Alpha1)
		if ok && volume.Name() == "EPHEMERAL" {
			assert.Equal(
				t,
				block.MustSize(maxEphemeral),
				volume.ProvisioningSpec.ProvisioningMaxSize,
			)

			return
		}
	}

	t.Fatal("EPHEMERAL configuration missing")
}

func TestInvalidAutoscalerPoolPatchStopsBeforeProvisioning(t *testing.T) {
	t.Parallel()

	configs, err := talosconfigmanager.NewDefaultConfigs()
	require.NoError(t, err)

	prov := talosprovisioner.NewProvisioner(configs, nil).
		WithHetznerOptions(ksailv1alpha1.OptionsHetzner{
			NodeAutoscalerEnabled: true,
			AutoscalerNodePools: []ksailv1alpha1.NodePool{{
				Name:         autoscalerTestPool,
				TalosPatches: []string{"machine: ["},
			}},
		})

	_, err = prov.BuildAutoscalerPoolConfigsForTest(configs.Bundle())
	require.ErrorContains(t, err, "pool machine config patch 1")
	require.ErrorContains(t, prov.Create(t.Context(), ""), "pool machine config patch 1")

	_, err = prov.DiffConfig(t.Context(), "", nil, nil)
	require.ErrorContains(t, err, "pool machine config patch 1")
	require.ErrorContains(t, prov.UpgradeDistribution(t.Context(), "", "v1.13.12", "v1.13.12"),
		"pool machine config patch 1")
}

func poolRawVolumePatch(name, size string) string {
	return "apiVersion: v1alpha1\nkind: RawVolumeConfig\nname: " + name + "\n" +
		"provisioning:\n  diskSelector:\n    match: system_disk\n  maxSize: " + size + "\n"
}

func rawVolumeSizes(cfg talosconfig.Provider) map[string]block.Size {
	sizes := make(map[string]block.Size)

	for _, document := range cfg.Documents() {
		if raw, ok := document.(*block.RawVolumeConfigV1Alpha1); ok {
			sizes[raw.Name()] = raw.ProvisioningSpec.ProvisioningMaxSize
		}
	}

	return sizes
}
