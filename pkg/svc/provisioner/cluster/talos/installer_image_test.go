package talosprovisioner_test

import (
	"io"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testTalosVersion = "v1.13.3"

// TestResolveInstallerImageNoSchematic verifies the upgrade installer falls back
// to the bare upstream installer when no schematic is configured (regression
// guard for the default, extension-less cluster).
func TestResolveInstallerImageNoSchematic(t *testing.T) {
	t.Parallel()

	provisioner := talosprovisioner.NewProvisioner(nil, nil).WithLogWriter(io.Discard)

	got := provisioner.ResolveInstallerImageForTest(testTalosVersion)

	assert.Equal(t, "ghcr.io/siderolabs/installer:"+testTalosVersion, got)
	assert.False(t, provisioner.HasSchematicConfiguredForTest())
	assert.Empty(t, provisioner.ResolveSchematicIDForTest())
}

// TestResolveInstallerImageNoSchematicTalos14 verifies extension-less Talos
// 1.14 upgrades use the Image Factory's empty metal installer schematic. Talos
// 1.14 no longer publishes ghcr.io/siderolabs/installer release images.
func TestResolveInstallerImageNoSchematicTalos14(t *testing.T) {
	t.Parallel()

	provisioner := talosprovisioner.NewProvisioner(nil, nil).WithLogWriter(io.Discard)

	got := provisioner.ResolveInstallerImageForTest("v1.14.0-alpha.2")

	assert.Equal(
		t,
		"factory.talos.dev/metal-installer/"+
			"376567988ad370138ad8b2698212367b8edcb69b5fd68c80be1f2ec7d603b4ba:"+
			"v1.14.0-alpha.2",
		got,
	)
	assert.False(t, provisioner.HasSchematicConfiguredForTest())
}

// TestResolveInstallerImageExplicitSchematic verifies that an explicit
// talosOpts.SchematicID produces the Image Factory installer so the rolling OS
// upgrade preserves system extensions (issue #5077, problem 1).
func TestResolveInstallerImageExplicitSchematic(t *testing.T) {
	t.Parallel()

	const schematicID = "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"

	provisioner := talosprovisioner.NewProvisioner(nil, nil).
		WithLogWriter(io.Discard).
		WithTalosOptsForTest(&v1alpha1.OptionsTalos{SchematicID: schematicID})

	got := provisioner.ResolveInstallerImageForTest(testTalosVersion)

	assert.Equal(
		t,
		"factory.talos.dev/installer/"+schematicID+":"+testTalosVersion,
		got,
	)
	assert.True(t, provisioner.HasSchematicConfiguredForTest())
	assert.Equal(t, schematicID, provisioner.ResolveSchematicIDForTest())
}

// TestResolveInstallerImageFromExtensions verifies that a schematic auto-computed
// from spec.cluster.talos.extensions (via talosConfigs) produces the Image Factory
// installer — the path that was previously dropped on upgrade, stripping
// extensions like iscsi-tools/qemu-guest-agent (issue #5077, problem 1).
func TestResolveInstallerImageFromExtensions(t *testing.T) {
	t.Parallel()

	configs, err := talosconfigmanager.
		NewConfigManager("", "ext-upgrade", "1.32.0", "10.5.0.0/24").
		WithExtensions([]string{"siderolabs/iscsi-tools", "siderolabs/qemu-guest-agent"}).
		Load(configmanager.LoadOptions{})
	require.NoError(t, err)

	schematicID := configs.SchematicID()
	require.Len(t, schematicID, 64, "extensions should auto-compute a schematic ID")

	provisioner := talosprovisioner.NewProvisioner(configs, nil).WithLogWriter(io.Discard)

	got := provisioner.ResolveInstallerImageForTest(testTalosVersion)

	assert.Equal(
		t,
		"factory.talos.dev/installer/"+schematicID+":"+testTalosVersion,
		got,
	)
	assert.True(t, strings.HasPrefix(got, "factory.talos.dev/installer/"))
	assert.Equal(t, schematicID, provisioner.ResolveSchematicIDForTest())
}

// TestResolveInstallerImageExplicitSchematicWinsOverExtensions verifies the
// resolution precedence: an explicit talosOpts.SchematicID takes priority over a
// schematic auto-computed from extensions, matching the snapshot/create path.
func TestResolveInstallerImageExplicitSchematicWinsOverExtensions(t *testing.T) {
	t.Parallel()

	const explicitID = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"

	configs, err := talosconfigmanager.
		NewConfigManager("", "ext-precedence", "1.32.0", "10.5.0.0/24").
		WithExtensions([]string{"siderolabs/iscsi-tools"}).
		Load(configmanager.LoadOptions{})
	require.NoError(t, err)
	require.NotEqual(t, explicitID, configs.SchematicID())

	provisioner := talosprovisioner.NewProvisioner(configs, nil).
		WithLogWriter(io.Discard).
		WithTalosOptsForTest(&v1alpha1.OptionsTalos{SchematicID: explicitID})

	got := provisioner.ResolveInstallerImageForTest(testTalosVersion)

	assert.Equal(t, "factory.talos.dev/installer/"+explicitID+":"+testTalosVersion, got)
	assert.Equal(t, explicitID, provisioner.ResolveSchematicIDForTest())
}
