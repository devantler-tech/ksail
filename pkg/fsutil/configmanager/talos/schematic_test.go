package talos_test

import (
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
		assertSameID(t, []string{"siderolabs/iscsi-tools"}, []string{"", "siderolabs/iscsi-tools", ""})
	})
	t.Run("deduplicates extensions", func(t *testing.T) {
		t.Parallel()
		assertSameID(t, []string{"siderolabs/iscsi-tools"}, []string{"siderolabs/iscsi-tools", "siderolabs/iscsi-tools"})
	})
}

func assertSameID(t *testing.T, first, second []string) {
	t.Helper()

	id1, err := talos.NewSchematic(first).ID()
	require.NoError(t, err)

	id2, err := talos.NewSchematic(second).ID()
	require.NoError(t, err)

	assert.Equal(t, id1, id2)
}

func assertDifferentIDs(t *testing.T, first, second []string) {
	t.Helper()

	id1, err := talos.NewSchematic(first).ID()
	require.NoError(t, err)

	id2, err := talos.NewSchematic(second).ID()
	require.NoError(t, err)

	assert.NotEqual(t, id1, id2)
}

func assertValidHexID(t *testing.T, extensions []string) {
	t.Helper()

	id, err := talos.NewSchematic(extensions).ID()
	require.NoError(t, err)
	assert.Len(t, id, 64, "SHA256 hex encoding should be 64 chars")
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
		configs := loadWithExtensions(t, []string{"siderolabs/iscsi-tools", "siderolabs/util-linux-tools"})
		assert.Len(t, configs.SchematicID(), 64)
	})
	t.Run("install image is patched on control plane config", func(t *testing.T) {
		t.Parallel()
		configs := loadWithExtensions(t, []string{"siderolabs/iscsi-tools"})
		assertInstallerImage(t, configs.ControlPlane().Machine().Install().Image(), configs.SchematicID())
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
