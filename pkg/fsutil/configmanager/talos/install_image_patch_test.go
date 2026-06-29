package talos_test

import (
	"testing"

	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// installImagePatch builds a cluster-scope strategic-merge patch setting machine.install.image.
func installImagePatch(name, image string) talos.Patch {
	return talos.Patch{
		Path:    "talos/cluster/" + name,
		Scope:   talos.PatchScopeCluster,
		Content: []byte("machine:\n  install:\n    image: " + image + "\n"),
	}
}

// loadWithPatch builds a Talos Configs carrying the given additional patch content (if any).
func loadWithPatch(t *testing.T, content string) *talos.Configs {
	t.Helper()

	manager := talos.NewConfigManager("", "patch-test", "1.32.0", "10.5.0.0/24")
	if content != "" {
		manager = manager.WithAdditionalPatches([]talos.Patch{{
			Path:    "talos/cluster/patch.yaml",
			Scope:   talos.PatchScopeCluster,
			Content: []byte(content),
		}})
	}

	configs, err := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err)

	return configs
}

func TestConfigsInstallImagePatch(t *testing.T) {
	t.Parallel()

	t.Run("returns image and true for a strategic-merge install-image patch", func(t *testing.T) {
		t.Parallel()

		configs := loadWithPatch(
			t,
			"machine:\n  install:\n    image: factory.talos.dev/installer/deadbeef:v1.13.4\n",
		)

		image, ok := configs.InstallImagePatch()
		assert.True(t, ok)
		assert.Equal(t, "factory.talos.dev/installer/deadbeef:v1.13.4", image)
	})

	t.Run("returns false for a strategic-merge patch without install image", func(t *testing.T) {
		t.Parallel()

		configs := loadWithPatch(t, "machine:\n  network:\n    hostname: node-1\n")

		image, ok := configs.InstallImagePatch()
		assert.False(t, ok)
		assert.Empty(t, image)
	})

	t.Run("returns false when there are no additional patches", func(t *testing.T) {
		t.Parallel()

		configs := loadWithPatch(t, "")

		image, ok := configs.InstallImagePatch()
		assert.False(t, ok)
		assert.Empty(t, image)
	})

	t.Run("returns the effective (last) image when multiple patches set it", func(t *testing.T) {
		t.Parallel()

		manager := talos.NewConfigManager("", "patch-test", "1.32.0", "10.5.0.0/24").
			WithAdditionalPatches([]talos.Patch{
				installImagePatch(
					"install-image-a.yaml",
					"factory.talos.dev/installer/aaa:v1.13.4",
				),
				installImagePatch(
					"install-image-b.yaml",
					"factory.talos.dev/installer/bbb:v1.13.4",
				),
			})

		configs, err := manager.Load(configmanager.LoadOptions{})
		require.NoError(t, err)

		image, ok := configs.InstallImagePatch()
		assert.True(t, ok)
		assert.Equal(t, "factory.talos.dev/installer/bbb:v1.13.4", image)
	})
}
