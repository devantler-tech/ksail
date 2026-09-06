package workload_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newKubeconfigFlagCommand builds a command carrying the kubectl --kubeconfig
// flag, optionally marked as explicitly set by the user.
func newKubeconfigFlagCommand(
	t *testing.T,
	value string,
	changed bool,
) (*cobra.Command, *pflag.Flag) {
	t.Helper()

	cmd := &cobra.Command{Use: "wait"}
	cmd.Flags().String("kubeconfig", "", "Path to the kubeconfig file to use for CLI requests.")

	if changed {
		require.NoError(t, cmd.Flags().Set("kubeconfig", value))
	}

	flag := cmd.Flags().Lookup("kubeconfig")
	require.NotNil(t, flag)

	return cmd, flag
}

// TestResolveKubeconfigFlag_CanonicalizesExplicitPath verifies that a kubeconfig
// the user passed is canonicalized before client-go opens it: a symlink resolves
// to its target, so a path pointing outside the intended location cannot reach
// the filesystem unresolved (AGENTS.md's canonicalization rule).
func TestResolveKubeconfigFlag_CanonicalizesExplicitPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "real-kubeconfig")
	require.NoError(t, os.WriteFile(target, []byte("apiVersion: v1\n"), 0o600))

	link := filepath.Join(dir, "link-kubeconfig")
	require.NoError(t, os.Symlink(target, link))

	resolvedTarget, err := filepath.EvalSymlinks(target)
	require.NoError(t, err)

	cmd, flag := newKubeconfigFlagCommand(t, link, true)

	require.NoError(t, workload.ExportResolveKubeconfigFlag(cmd, flag, "/config/derived/path"))

	assert.Equal(t, resolvedTarget, flag.Value.String(),
		"an explicit --kubeconfig must be canonicalized, not used raw")
}

// TestResolveKubeconfigFlag_CanonicalizesRelativePath verifies the same for a
// relative path, which client-go would otherwise resolve against the process's
// working directory at open time.
//
//nolint:paralleltest // uses t.Chdir to set the working directory
func TestResolveKubeconfigFlag_CanonicalizesRelativePath(t *testing.T) {
	dir := t.TempDir()
	name := "relative-kubeconfig"
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("apiVersion: v1\n"), 0o600))

	t.Chdir(dir)

	cmd, flag := newKubeconfigFlagCommand(t, name, true)

	require.NoError(t, workload.ExportResolveKubeconfigFlag(cmd, flag, "/config/derived/path"))

	assert.True(t, filepath.IsAbs(flag.Value.String()),
		"a relative --kubeconfig must become absolute, got %q", flag.Value.String())
	assert.Contains(t, flag.Value.String(), name)
}

// TestResolveKubeconfigFlag_UnsetTakesResolvedPath verifies the existing
// behaviour is preserved: when the user did not pass the flag, it takes the path
// KSail resolved from the config, and that value also becomes the default.
func TestResolveKubeconfigFlag_UnsetTakesResolvedPath(t *testing.T) {
	t.Parallel()

	cmd, flag := newKubeconfigFlagCommand(t, "", false)

	require.NoError(t, workload.ExportResolveKubeconfigFlag(cmd, flag, "/config/derived/path"))

	assert.Equal(t, "/config/derived/path", flag.Value.String())
	assert.Equal(t, "/config/derived/path", flag.DefValue,
		"an unset flag's default must show the resolved path in --help")
}

// TestResolveKubeconfigFlag_MissingExplicitFileStillResolves verifies a
// not-yet-existing kubeconfig is canonicalized via its parent rather than
// rejected here, so the failure still comes from client-go with its own message.
func TestResolveKubeconfigFlag_MissingExplicitFileStillResolves(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	missing := filepath.Join(dir, "absent-kubeconfig")

	cmd, flag := newKubeconfigFlagCommand(t, missing, true)

	require.NoError(t, workload.ExportResolveKubeconfigFlag(cmd, flag, "/config/derived/path"))

	// The whole canonical path is asserted: a Contains check on the basename
	// would also pass if resolution picked a different parent directory.
	canonicalDir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(canonicalDir, "absent-kubeconfig"), flag.Value.String())
}

// TestResolveKubeconfigFlag_ExplicitEmptyStaysEmpty verifies that `--kubeconfig ""`
// is left alone. Canonicalizing an empty string yields the working directory,
// which client-go would then try to load as a kubeconfig file.
func TestResolveKubeconfigFlag_ExplicitEmptyStaysEmpty(t *testing.T) {
	t.Parallel()

	cmd, flag := newKubeconfigFlagCommand(t, "", true)

	require.NoError(t, workload.ExportResolveKubeconfigFlag(cmd, flag, "/config/derived/path"))

	assert.Empty(t, flag.Value.String(),
		"an explicitly empty --kubeconfig must stay empty, not become the working directory")
}
