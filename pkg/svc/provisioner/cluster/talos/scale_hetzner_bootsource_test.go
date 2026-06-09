package talosprovisioner_test

import (
	"io"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
)

// TestHetznerBootSource_SnapshotTakesPrecedence verifies a configured snapshot image
// suppresses the ISO so a new node boots the cluster's Talos version, and that the ISO
// is used only when no snapshot is configured. This guards the scale-up/rolling-recreate
// regression where new Hetzner nodes booted the older maintenance ISO (Talos 1.12.4) and
// rejected newer config documents (e.g. ImageVerificationConfig, Talos 1.13+) with
// `"ImageVerificationConfig" "v1alpha1": not registered`.
func TestHetznerBootSource_SnapshotTakesPrecedence(t *testing.T) {
	t.Parallel()

	const isoID, snapshotID = int64(125127), int64(424242)

	iso, image := talosprovisioner.HetznerBootSource(isoID, snapshotID)
	assert.Zero(t, iso, "ISO must be suppressed when a snapshot image is configured")
	assert.Equal(t, snapshotID, image, "node must boot from the snapshot image")

	iso, image = talosprovisioner.HetznerBootSource(isoID, 0)
	assert.Equal(t, isoID, iso, "node must fall back to the ISO when no snapshot is configured")
	assert.Zero(t, image, "no snapshot image when none is configured")
}

// TestHetznerScaleServerOpts_BootsFromSnapshotWhenConfigured verifies the scale-up and
// rolling-recreate server options boot a new node from the cluster's Talos snapshot image
// (ISO suppressed) when one is configured, rather than the default maintenance ISO. Booting
// the ISO is what made scaling a Talos 1.13 cluster fail when its config carried a 1.13+
// document the 1.12.4 ISO could not parse.
func TestHetznerScaleServerOpts_BootsFromSnapshotWhenConfigured(t *testing.T) {
	t.Parallel()

	const snapshotID = int64(987654)

	prov := talosprovisioner.NewProvisioner(nil, nil).
		WithLogWriter(io.Discard).
		WithTalosOptions(v1alpha1.OptionsTalos{ISO: 125127}).
		WithHetznerOptions(v1alpha1.OptionsHetzner{WorkerServerType: "cx43", Location: "fsn1"})

	opts := prov.HetznerScaleServerOptsForTest(
		"prod", talosprovisioner.RoleWorker, "prod-worker-4", 4,
		talosprovisioner.HetznerInfra{NetworkID: 1, FirewallID: 2}, snapshotID,
	)

	assert.Equal(t, snapshotID, opts.ImageID, "scaled node must boot from the snapshot image")
	assert.Zero(t, opts.ISOID, "the maintenance ISO must be suppressed when a snapshot exists")
	assert.Equal(
		t,
		"cx43",
		opts.ServerType,
		"scaled node must use the configured worker server type",
	)
}

// TestHetznerScaleServerOpts_FallsBackToISO verifies that without a snapshot image the
// scaled node boots the configured maintenance ISO (legacy, schematic-less clusters).
func TestHetznerScaleServerOpts_FallsBackToISO(t *testing.T) {
	t.Parallel()

	prov := talosprovisioner.NewProvisioner(nil, nil).
		WithLogWriter(io.Discard).
		WithTalosOptions(v1alpha1.OptionsTalos{ISO: 125127}).
		WithHetznerOptions(v1alpha1.OptionsHetzner{WorkerServerType: "cx43"})

	opts := prov.HetznerScaleServerOptsForTest(
		"prod", talosprovisioner.RoleWorker, "prod-worker-4", 4,
		talosprovisioner.HetznerInfra{}, 0,
	)

	assert.Equal(
		t,
		int64(125127),
		opts.ISOID,
		"node must boot the ISO when no snapshot is configured",
	)
	assert.Zero(t, opts.ImageID, "no snapshot image when none is configured")
}
