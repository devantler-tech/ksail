package talos_test

import (
	"encoding/hex"
	"testing"

	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSchematic(t *testing.T) {
	t.Parallel()

	t.Run("sorts extensions for deterministic IDs", func(t *testing.T) {
		t.Parallel()
		assertSameID(t,
			[]string{"siderolabs/util-linux-tools", "siderolabs/iscsi-tools"},
			[]string{"siderolabs/iscsi-tools", "siderolabs/util-linux-tools"},
		)
	})
	t.Run("different extensions produce different IDs", func(t *testing.T) {
		t.Parallel()
		assertDifferentIDs(t, []string{"siderolabs/iscsi-tools"}, []string{"siderolabs/gvisor"})
	})
	t.Run("single extension produces a valid hex ID", func(t *testing.T) {
		t.Parallel()
		assertValidHexID(t, []string{"siderolabs/iscsi-tools"})
	})
	t.Run("empty extensions produce a deterministic ID", func(t *testing.T) {
		t.Parallel()
		assertValidHexID(t, nil)
	})
	t.Run("normalizes whitespace in extensions", func(t *testing.T) {
		t.Parallel()
		assertSameID(t, []string{"siderolabs/iscsi-tools"}, []string{"  siderolabs/iscsi-tools  "})
	})
	t.Run("drops empty strings", func(t *testing.T) {
		t.Parallel()
		assertSameID(
			t,
			[]string{"siderolabs/iscsi-tools"},
			[]string{"", "siderolabs/iscsi-tools", ""},
		)
	})
	t.Run("deduplicates extensions", func(t *testing.T) {
		t.Parallel()
		assertSameID(
			t,
			[]string{"siderolabs/iscsi-tools"},
			[]string{"siderolabs/iscsi-tools", "siderolabs/iscsi-tools"},
		)
	})
}

func TestNewSchematicKernelArgs(t *testing.T) {
	t.Parallel()

	exts := []string{"siderolabs/iscsi-tools"}

	t.Run("kernel args change the schematic ID", func(t *testing.T) {
		t.Parallel()
		without := mustID(t, talos.NewSchematic(exts, nil))
		with := mustID(t, talos.NewSchematic(exts, []string{"security=apparmor"}))
		assert.NotEqual(t, without, with)
	})
	t.Run("same kernel args produce the same ID", func(t *testing.T) {
		t.Parallel()
		first := mustID(t, talos.NewSchematic(exts, []string{"security=apparmor"}))
		second := mustID(t, talos.NewSchematic(exts, []string{"security=apparmor"}))
		assert.Equal(t, first, second)
	})
	t.Run("kernel arg order is significant", func(t *testing.T) {
		t.Parallel()
		// Unlike extensions, kernel args are order-preserving (not sorted), so a
		// different declared order is a different schematic.
		forward := mustID(t, talos.NewSchematic(exts, []string{"a=1", "b=2"}))
		reverse := mustID(t, talos.NewSchematic(exts, []string{"b=2", "a=1"}))
		assert.NotEqual(t, forward, reverse)
	})
	t.Run("normalizes whitespace and drops empty kernel args", func(t *testing.T) {
		t.Parallel()
		clean := mustID(t, talos.NewSchematic(exts, []string{"security=apparmor"}))
		messy := mustID(t, talos.NewSchematic(exts, []string{"", "  security=apparmor  ", ""}))
		assert.Equal(t, clean, messy)
	})
	t.Run("kernel-args-only produces a valid hex ID", func(t *testing.T) {
		t.Parallel()
		id := mustID(t, talos.NewSchematic(nil, []string{"security=apparmor"}))
		assert.Len(t, id, 64, "SHA256 hex encoding should be 64 chars")
		_, decodeErr := hex.DecodeString(id)
		assert.NoError(t, decodeErr, "ID should contain only valid hex characters")
	})
}

func mustID(t *testing.T, schematic *talos.Schematic) string {
	t.Helper()

	id, err := schematic.ID()
	require.NoError(t, err)

	return id
}

func assertSameID(t *testing.T, first, second []string) {
	t.Helper()

	id1, err := talos.NewSchematic(first, nil).ID()
	require.NoError(t, err)

	id2, err := talos.NewSchematic(second, nil).ID()
	require.NoError(t, err)

	assert.Equal(t, id1, id2)
}

func assertDifferentIDs(t *testing.T, first, second []string) {
	t.Helper()

	id1, err := talos.NewSchematic(first, nil).ID()
	require.NoError(t, err)

	id2, err := talos.NewSchematic(second, nil).ID()
	require.NoError(t, err)

	assert.NotEqual(t, id1, id2)
}

func assertValidHexID(t *testing.T, extensions []string) {
	t.Helper()

	id, err := talos.NewSchematic(extensions, nil).ID()
	require.NoError(t, err)
	assert.Len(t, id, 64, "SHA256 hex encoding should be 64 chars")
	_, decodeErr := hex.DecodeString(id)
	assert.NoError(t, decodeErr, "ID should contain only valid hex characters")
}

func TestSchematicInstallerImage(t *testing.T) {
	t.Parallel()

	t.Run("constructs correct image reference", func(t *testing.T) {
		t.Parallel()

		image := talos.SchematicInstallerImage("abc123def456", "v1.11.2")
		assert.Equal(t, "factory.talos.dev/installer/abc123def456:v1.11.2", image)
	})
}

func TestConfigsWithExtensions(t *testing.T) {
	t.Parallel()

	t.Run("SchematicID is empty when no extensions", func(t *testing.T) {
		t.Parallel()
		configs := loadWithExtensions(t, nil)
		assert.Empty(t, configs.SchematicID())
		assert.Nil(t, configs.Extensions())
	})
	t.Run("SchematicID is set when extensions configured", func(t *testing.T) {
		t.Parallel()
		configs := loadWithExtensions(
			t,
			[]string{"siderolabs/iscsi-tools", "siderolabs/util-linux-tools"},
		)
		assert.Len(t, configs.SchematicID(), 64)
	})
	t.Run("install image is patched on control plane config", func(t *testing.T) {
		t.Parallel()
		configs := loadWithExtensions(t, []string{"siderolabs/iscsi-tools"})
		assertInstallerImage(
			t,
			configs.ControlPlane().Machine().Install().Image(),
			configs.SchematicID(),
		)
	})
	t.Run("install image is patched on worker config", func(t *testing.T) {
		t.Parallel()
		configs := loadWithExtensions(t, []string{"siderolabs/iscsi-tools"})
		assertInstallerImage(t, configs.Worker().Machine().Install().Image(), configs.SchematicID())
	})
	t.Run("schematic preserved across WithName", func(t *testing.T) {
		t.Parallel()
		configs := loadWithExtensions(t, []string{"siderolabs/iscsi-tools"})
		renamed, err := configs.WithName("new-name")
		require.NoError(t, err)
		assert.Equal(t, configs.SchematicID(), renamed.SchematicID())
	})
	t.Run("schematic preserved across WithEndpoint", func(t *testing.T) {
		t.Parallel()
		configs := loadWithExtensions(t, []string{"siderolabs/iscsi-tools"})
		updated, err := configs.WithEndpoint("1.2.3.4")
		require.NoError(t, err)
		assert.Equal(t, configs.SchematicID(), updated.SchematicID())
	})
}

func loadWithExtensions(t *testing.T, extensions []string) *talos.Configs {
	t.Helper()

	manager := talos.NewConfigManager("", "ext-test", "1.32.0", "10.5.0.0/24")
	if len(extensions) > 0 {
		manager = manager.WithExtensions(extensions)
	}

	configs, err := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err)

	return configs
}

func assertInstallerImage(t *testing.T, image, schematicID string) {
	t.Helper()
	assert.Contains(t, image, "factory.talos.dev/installer/")
	assert.Contains(t, image, schematicID)
}

func TestConfigsWithKernelArgs(t *testing.T) {
	t.Parallel()

	t.Run("SchematicID set and install image patched with kernel-args only", func(t *testing.T) {
		t.Parallel()
		configs := loadWithSchematic(t, nil, []string{"security=apparmor"})
		assert.Len(t, configs.SchematicID(), 64)
		assert.Equal(t, []string{"security=apparmor"}, configs.ExtraKernelArgs())
		assertInstallerImage(
			t,
			configs.ControlPlane().Machine().Install().Image(),
			configs.SchematicID(),
		)
	})
	t.Run("kernel args change the SchematicID vs extensions-only", func(t *testing.T) {
		t.Parallel()
		base := loadWithSchematic(t, []string{"siderolabs/iscsi-tools"}, nil)
		withArgs := loadWithSchematic(
			t,
			[]string{"siderolabs/iscsi-tools"},
			[]string{"security=apparmor"},
		)
		assert.NotEqual(t, base.SchematicID(), withArgs.SchematicID())
	})
	t.Run("kernel args preserved across WithName", func(t *testing.T) {
		t.Parallel()
		configs := loadWithSchematic(t, nil, []string{"security=apparmor"})
		renamed, err := configs.WithName("new-name")
		require.NoError(t, err)
		assert.Equal(t, configs.SchematicID(), renamed.SchematicID())
		assert.Equal(t, configs.ExtraKernelArgs(), renamed.ExtraKernelArgs())
	})
	t.Run("kernel args preserved across WithEndpoint", func(t *testing.T) {
		t.Parallel()
		configs := loadWithSchematic(t, nil, []string{"security=apparmor"})
		updated, err := configs.WithEndpoint("1.2.3.4")
		require.NoError(t, err)
		assert.Equal(t, configs.SchematicID(), updated.SchematicID())
		assert.Equal(t, configs.ExtraKernelArgs(), updated.ExtraKernelArgs())
	})
}

func loadWithSchematic(t *testing.T, extensions, kernelArgs []string) *talos.Configs {
	t.Helper()

	manager := talos.NewConfigManager("", "ext-test", "1.32.0", "10.5.0.0/24")
	if len(extensions) > 0 {
		manager = manager.WithExtensions(extensions)
	}

	if len(kernelArgs) > 0 {
		manager = manager.WithKernelArgs(kernelArgs)
	}

	configs, err := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err)

	return configs
}
