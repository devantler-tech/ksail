package tenant_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	tenantcmd "github.com/devantler-tech/ksail/v7/pkg/cli/cmd/tenant"
	"github.com/devantler-tech/ksail/v7/pkg/di"
	"github.com/stretchr/testify/require"
)

func TestDeleteCmd(t *testing.T) {
	t.Parallel()

	t.Run("has write permission annotation", func(t *testing.T) {
		t.Parallel()

		cmd := tenantcmd.NewDeleteCmd(nil)
		require.Equal(t, "write", cmd.Annotations["ai.toolgen.permission"])
	})

	t.Run("requires exactly one arg", func(t *testing.T) {
		t.Parallel()

		cmd := tenantcmd.NewDeleteCmd(nil)
		cmd.SetArgs([]string{})

		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		err := cmd.Execute()
		require.Error(t, err)
	})

	t.Run("flag defaults", assertDeleteFlagDefaults)

	t.Run("deletes tenant directory", assertDeletesTenantDirectory)

	t.Run("returns error for non-existent tenant", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()

		cmd := tenantcmd.NewDeleteCmd(&di.Runtime{})
		cmd.SetArgs([]string{
			"no-such-tenant",
			"--output", tmpDir,
			"--force",
		})

		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)

		err := cmd.Execute()
		require.Error(t, err)
		require.Contains(t, err.Error(), "does not exist")
	})
}

func assertDeleteFlagDefaults(t *testing.T) {
	t.Parallel()

	cmd := tenantcmd.NewDeleteCmd(nil)

	forceVal, err := cmd.Flags().GetBool("force")
	require.NoError(t, err)
	require.False(t, forceVal)

	unregister, err := cmd.Flags().GetBool("unregister")
	require.NoError(t, err)
	require.True(t, unregister)

	deleteRepo, err := cmd.Flags().GetBool("delete-repo")
	require.NoError(t, err)
	require.False(t, deleteRepo)

	output, err := cmd.Flags().GetString("output")
	require.NoError(t, err)
	require.Equal(t, ".", output)
}

func assertDeletesTenantDirectory(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	tenantDir := filepath.Join(tmpDir, "my-tenant")
	require.NoError(t, os.MkdirAll(tenantDir, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(tenantDir, "namespace.yaml"),
		[]byte("test"), 0o600,
	))

	cmd := tenantcmd.NewDeleteCmd(&di.Runtime{})
	cmd.SetArgs([]string{
		"my-tenant",
		"--output", tmpDir,
		"--force",
		"--unregister=false",
	})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	require.NoError(t, err)

	_, statErr := os.Stat(tenantDir)
	require.True(t, os.IsNotExist(statErr))
}
