package talosprovisioner_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// talosVersionLifecycleGA is the first Talos release whose nodes implement the
// LifecycleService/ImageService upgrade APIs.
const talosVersionLifecycleGA = "v1.13.0"

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

// TestUpgradeDistribution_ContainerModeSkipped verifies that UpgradeDistribution
// short-circuits with clustererr.ErrUpgradeSkipped for providers that cannot
// perform an in-place Talos OS upgrade:
//   - Docker (container mode): Talos masks out the Upgrade capability, so both the
//     legacy MachineService.Upgrade and the LifecycleService.Upgrade RPCs reject
//     with "method is not supported in container mode". The cluster must be
//     recreated to change versions. This is the regression guard for the
//     Talos×Docker system-test failure when a newer Talos patch (e.g. v1.13.4) is
//     released and the discovery-driven update tries to upgrade in place.
//   - Omni: upgrades are managed externally by Omni.
//
// Hetzner runs on real machines and must NOT be skipped — it proceeds to the
// rolling upgrade (which is a no-op here because no infrastructure provider is
// wired, so node listing returns an empty set).
func TestUpgradeDistribution_ContainerModeSkipped(t *testing.T) {
	t.Parallel()

	t.Run("docker provider is skipped", func(t *testing.T) {
		t.Parallel()

		// A bare provisioner has neither Hetzner nor Omni options, so it routes to
		// the Docker (container-mode) provider — mirroring Create/Delete/Exists.
		provisioner := talosprovisioner.NewProvisioner(nil, nil)

		err := provisioner.UpgradeDistribution(
			context.Background(), "test-cluster", "v1.13.3", "v1.13.4",
		)

		require.ErrorIs(t, err, clustererr.ErrUpgradeSkipped)
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

	t.Run("hetzner provider is not skipped", func(t *testing.T) {
		t.Parallel()

		provisioner := talosprovisioner.NewProvisioner(nil, nil).
			WithHetznerOptions(v1alpha1.OptionsHetzner{})

		err := provisioner.UpgradeDistribution(
			context.Background(), "test-cluster", "v1.13.3", "v1.13.4",
		)

		// Hetzner clusters run on real machines and can upgrade in place, so the
		// Docker/Omni skip must not fire. With no infrastructure provider wired,
		// node listing yields an empty set and the rolling upgrade is a no-op.
		require.NotErrorIs(t, err, clustererr.ErrUpgradeSkipped)
	})
}
