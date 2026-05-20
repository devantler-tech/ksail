package vclusterprovisioner_test

import (
	"os"
	"testing"

	vclusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/vcluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildValuesFiles_ExtraSAN(t *testing.T) {
	t.Parallel()

	t.Run("adds proxy extraSANs when address provided", func(t *testing.T) {
		t.Parallel()

		files, cleanup, err := vclusterprovisioner.BuildValuesFilesForTest("", false, "5.6.7.8")
		require.NoError(t, err)

		defer cleanup()

		require.NotEmpty(t, files)

		content, err := os.ReadFile(files[0])
		require.NoError(t, err)

		assert.Contains(t, string(content), "proxy:")
		assert.Contains(t, string(content), "extraSANs:")
		assert.Contains(t, string(content), "5.6.7.8")
	})

	t.Run("omits proxy extraSANs when address empty", func(t *testing.T) {
		t.Parallel()

		files, cleanup, err := vclusterprovisioner.BuildValuesFilesForTest("", false, "")
		require.NoError(t, err)

		defer cleanup()

		require.NotEmpty(t, files)

		content, err := os.ReadFile(files[0])
		require.NoError(t, err)

		assert.NotContains(t, string(content), "extraSANs:")
	})
}
