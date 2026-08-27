package talosprovisioner_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// talosVersionLifecycleGA is the first Talos release whose nodes implement the
// LifecycleService/ImageService upgrade APIs.
const talosVersionLifecycleGA = "v1.13.0"

func TestKubernetesImageRefUsesTalosKubeletAvailability(t *testing.T) {
	t.Parallel()

	provisioner := talosprovisioner.NewProvisioner(nil, nil)

	assert.Equal(t, "ghcr.io/siderolabs/kubelet", provisioner.KubernetesImageRef())
}

// TestSupportsLifecycleUpgradeAPI verifies that the upgrade path dispatch picks
// the LifecycleService/ImageService APIs only for Talos >= 1.13 and otherwise
// falls back to the legacy MachineService.Upgrade API. The v1.12.4 → false case
// is the regression guard for the reported "unknown service machine.ImageService"
// failure when upgrading a cluster still running an older Talos release.
func TestSupportsLifecycleUpgradeAPI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{name: "1.13 GA uses lifecycle API", version: talosVersionLifecycleGA, want: true},
		{name: "1.13 patch uses lifecycle API", version: "v1.13.3", want: true},
		{name: "newer minor uses lifecycle API", version: "v1.14.2", want: true},
		{name: "next major uses lifecycle API", version: "v2.0.0", want: true},
		{name: "tag without v prefix is parsed", version: "1.13.3", want: true},
		{name: "1.12 falls back to legacy (regression guard)", version: "v1.12.4", want: false},
		{name: "older 1.12 patch falls back to legacy", version: "v1.12.0", want: false},
		{name: "much older minor falls back to legacy", version: "v1.11.5", want: false},
		{name: "pre-1.13 alpha falls back to legacy", version: "v1.13.0-alpha.2", want: false},
		{name: "empty tag falls back to legacy", version: "", want: false},
		{name: "unparseable tag falls back to legacy", version: "not-a-version", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := talosprovisioner.SupportsLifecycleUpgradeAPIForTest(tt.version)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestUpgradeDistribution_ContainerMode verifies how UpgradeDistribution dispatches
// per provider. Container-mode (Docker) Talos nodes cannot upgrade their OS in
// place — Talos masks out the Upgrade capability for container mode, so both the
// legacy MachineService.Upgrade and the LifecycleService.Upgrade RPCs reject with
// "method is not supported in container mode". So:
//   - Docker, target within KSail's machinery support → ErrRecreationRequired
//     (the cluster is recreated to reach the new version, like Kind/K3d). This is
//     the regression guard for the Talos×Docker system-test failure when a newer
//     Talos patch is released and update would otherwise attempt an impossible
//     in-place upgrade.
//   - Docker, target newer than the vendored machinery → ErrUpgradeSkipped
//     (KSail cannot generate a config for a Talos release it does not vendor;
//     the user must update KSail). Uses a far-future version as the target.
//   - Omni → ErrUpgradeSkipped (managed externally by Omni).
//   - Hetzner → neither (real machines upgrade in place; it proceeds to the
//     rolling upgrade, a no-op here because no infrastructure provider is wired).
func TestUpgradeDistribution_ContainerMode(t *testing.T) {
	t.Parallel()

	t.Run("docker within support requires recreation", func(t *testing.T) {
		t.Parallel()

		// A bare provisioner has neither Hetzner nor Omni options, so it routes to
		// the Docker (container-mode) provider — mirroring Create/Delete/Exists.
		provisioner := talosprovisioner.NewProvisioner(nil, nil)

		err := provisioner.UpgradeDistribution(
			context.Background(), "test-cluster", "v1.13.3", "v1.13.4",
		)

		require.ErrorIs(t, err, clustererr.ErrRecreationRequired)
		require.NotErrorIs(t, err, clustererr.ErrUpgradeSkipped)
	})

	t.Run("docker above machinery support is skipped", func(t *testing.T) {
		t.Parallel()

		provisioner := talosprovisioner.NewProvisioner(nil, nil)

		// A version far above any plausible vendored machinery version cannot be
		// provisioned by this KSail build, so recreation is refused.
		err := provisioner.UpgradeDistribution(
			context.Background(), "test-cluster", "v1.13.3", "v99.0.0",
		)

		require.ErrorIs(t, err, clustererr.ErrUpgradeSkipped)
		require.NotErrorIs(t, err, clustererr.ErrRecreationRequired)
	})

	t.Run("omni provider is skipped", func(t *testing.T) {
		t.Parallel()

		provisioner := talosprovisioner.NewProvisioner(nil, nil).
			WithOmniOptions(v1alpha1.OptionsOmni{})

		err := provisioner.UpgradeDistribution(
			context.Background(), "test-cluster", "v1.13.3", "v1.13.4",
		)

		require.ErrorIs(t, err, clustererr.ErrUpgradeSkipped)
	})

	t.Run("hetzner provider is not skipped or recreated", func(t *testing.T) {
		t.Parallel()

		provisioner := talosprovisioner.NewProvisioner(nil, nil).
			WithHetznerOptions(v1alpha1.OptionsHetzner{})

		err := provisioner.UpgradeDistribution(
			context.Background(), "test-cluster", "v1.13.3", "v1.13.4",
		)

		// Hetzner clusters run on real machines and upgrade in place, so neither
		// the Docker recreate nor the skip path must fire. With no infrastructure
		// provider wired, node listing yields an empty set and the rolling upgrade
		// (including the pre-upgrade config reconcile) is a no-op.
		require.NotErrorIs(t, err, clustererr.ErrUpgradeSkipped)
		require.NotErrorIs(t, err, clustererr.ErrRecreationRequired)
	})
}

// TestReconcileNodeConfigBeforeUpgrade_NoopWithoutTalosConfig verifies the
// pre-upgrade config reconcile (issue #5294) is a no-op when no Talos config is
// loaded: there is nothing to reconcile, so it must return without error and without
// touching the node, letting the OS upgrade proceed unchanged.
func TestReconcileNodeConfigBeforeUpgrade_NoopWithoutTalosConfig(t *testing.T) {
	t.Parallel()

	prov := talosprovisioner.NewProvisioner(nil, nil)
	node := talosprovisioner.NewNodeWithRoleForTest(
		"10.0.0.2", talosprovisioner.RoleControlPlane,
	)

	err := prov.ReconcileNodeConfigBeforeUpgradeForTest(context.Background(), node, nil)
	require.NoError(t, err)
}

// TestReconcileNodeConfigBeforeUpgrade_BuildErrorPropagates verifies that a failure
// to build the node's desired config is surfaced (wrapped) rather than swallowed, so
// the rolling-upgrade caller can warn before proceeding. It drives the worker-role
// build path with no control-plane secrets source: a worker's running config carries
// no CA private key, so the realignment fails the control-plane-PKI precondition
// (#4963) before any live ApplyConfiguration RPC is attempted — exercising
// reconcileNodeConfigBeforeUpgrade up to, but not including, node I/O.
func TestReconcileNodeConfigBeforeUpgrade_BuildErrorPropagates(t *testing.T) {
	t.Parallel()

	desiredConfigs, err := talosconfigmanager.NewDefaultConfigsWithPatches(nil)
	require.NoError(t, err)

	workerRunning := runningWithHostname(t, talosprovisioner.RoleWorker, "prod-worker-1")

	prov := talosprovisioner.NewProvisioner(desiredConfigs, nil).
		WithNodeConfigFetcherForTest(
			func(_ context.Context, _ string) (talosconfig.Provider, error) {
				return workerRunning, nil
			},
		)

	node := talosprovisioner.NewNodeWithRoleForTest("10.0.0.3", talosprovisioner.RoleWorker)

	err = prov.ReconcileNodeConfigBeforeUpgradeForTest(context.Background(), node, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "build desired config",
		"the build failure must be wrapped so the caller can attribute it")
	assert.Contains(t, err.Error(), "control-plane PKI",
		"the actionable #4963 precondition must be surfaced")
}
