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

		s1 := talos.NewSchematic([]string{"siderolabs/util-linux-tools", "siderolabs/iscsi-tools"})
		s2 := talos.NewSchematic([]string{"siderolabs/iscsi-tools", "siderolabs/util-linux-tools"})

		id1, err1 := s1.ID()
		require.NoError(t, err1)

		id2, err2 := s2.ID()
		require.NoError(t, err2)

		assert.Equal(t, id1, id2, "IDs should be identical regardless of input order")
	})

	t.Run("different extensions produce different IDs", func(t *testing.T) {
		t.Parallel()

		s1 := talos.NewSchematic([]string{"siderolabs/iscsi-tools"})
		s2 := talos.NewSchematic([]string{"siderolabs/gvisor"})

		id1, err1 := s1.ID()
		require.NoError(t, err1)

		id2, err2 := s2.ID()
		require.NoError(t, err2)

		assert.NotEqual(t, id1, id2)
	})

	t.Run("single extension produces a valid hex ID", func(t *testing.T) {
		t.Parallel()

		s := talos.NewSchematic([]string{"siderolabs/iscsi-tools"})

		id, err := s.ID()
		require.NoError(t, err)
		assert.Len(t, id, 64, "SHA256 hex encoding should be 64 chars")
	})

	t.Run("empty extensions produce a deterministic ID", func(t *testing.T) {
		t.Parallel()

		s := talos.NewSchematic(nil)

		id, err := s.ID()
		require.NoError(t, err)
		assert.Len(t, id, 64)
	})
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

		manager := talos.NewConfigManager("", "ext-test", "1.32.0", "10.5.0.0/24")

		configs, err := manager.Load(configmanager.LoadOptions{})
		require.NoError(t, err)

		assert.Empty(t, configs.SchematicID())
		assert.Nil(t, configs.Extensions())
	})

	t.Run("SchematicID is set when extensions configured", func(t *testing.T) {
		t.Parallel()

		extensions := []string{"siderolabs/iscsi-tools", "siderolabs/util-linux-tools"}
		manager := talos.NewConfigManager("", "ext-test", "1.32.0", "10.5.0.0/24").
			WithExtensions(extensions)

		configs, err := manager.Load(configmanager.LoadOptions{})
		require.NoError(t, err)

		assert.NotEmpty(t, configs.SchematicID())
		assert.Len(t, configs.SchematicID(), 64)
		assert.Equal(t, extensions, configs.Extensions())
	})

	t.Run("install image is patched on control plane config", func(t *testing.T) {
		t.Parallel()

		extensions := []string{"siderolabs/iscsi-tools"}
		manager := talos.NewConfigManager("", "ext-test", "1.32.0", "10.5.0.0/24").
			WithExtensions(extensions)

		configs, err := manager.Load(configmanager.LoadOptions{})
		require.NoError(t, err)

		cp := configs.ControlPlane()
		require.NotNil(t, cp)

		installImage := cp.Machine().Install().Image()
		assert.Contains(t, installImage, "factory.talos.dev/installer/")
		assert.Contains(t, installImage, configs.SchematicID())
	})

	t.Run("install image is patched on worker config", func(t *testing.T) {
		t.Parallel()

		extensions := []string{"siderolabs/iscsi-tools"}
		manager := talos.NewConfigManager("", "ext-test", "1.32.0", "10.5.0.0/24").
			WithExtensions(extensions)

		configs, err := manager.Load(configmanager.LoadOptions{})
		require.NoError(t, err)

		w := configs.Worker()
		require.NotNil(t, w)

		installImage := w.Machine().Install().Image()
		assert.Contains(t, installImage, "factory.talos.dev/installer/")
		assert.Contains(t, installImage, configs.SchematicID())
	})

	t.Run("schematic preserved across WithName", func(t *testing.T) {
		t.Parallel()

		extensions := []string{"siderolabs/iscsi-tools"}
		manager := talos.NewConfigManager("", "original", "1.32.0", "10.5.0.0/24").
			WithExtensions(extensions)

		configs, err := manager.Load(configmanager.LoadOptions{})
		require.NoError(t, err)

		originalID := configs.SchematicID()
		require.NotEmpty(t, originalID)

		renamed, err := configs.WithName("new-name")
		require.NoError(t, err)

		assert.Equal(t, originalID, renamed.SchematicID())
		assert.Equal(t, extensions, renamed.Extensions())
	})

	t.Run("schematic preserved across WithEndpoint", func(t *testing.T) {
		t.Parallel()

		extensions := []string{"siderolabs/iscsi-tools"}
		manager := talos.NewConfigManager("", "endpoint-test", "1.32.0", "10.5.0.0/24").
			WithExtensions(extensions)

		configs, err := manager.Load(configmanager.LoadOptions{})
		require.NoError(t, err)

		originalID := configs.SchematicID()
		require.NotEmpty(t, originalID)

		updated, err := configs.WithEndpoint("1.2.3.4")
		require.NoError(t, err)

		assert.Equal(t, originalID, updated.SchematicID())
	})
}
