package cluster_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/stretchr/testify/require"
)

func TestConnect_CommandFlags(t *testing.T) {
	t.Parallel()

	cmd := cluster.NewConnectCmd()

	// Verify expected flags exist
	contextFlag := cmd.Flags().Lookup("context")
	require.NotNil(t, contextFlag, "expected --context flag")
	require.Equal(t, "c", contextFlag.Shorthand)

	kubeconfigFlag := cmd.Flags().Lookup("kubeconfig")
	require.NotNil(t, kubeconfigFlag, "expected --kubeconfig flag")
	require.Equal(t, "k", kubeconfigFlag.Shorthand)

	editorFlag := cmd.Flags().Lookup("editor")
	require.NotNil(t, editorFlag, "expected --editor flag")

	// Verify hidden flags exist but are hidden (needed for config defaults/validation)
	distributionFlag := cmd.Flags().Lookup("distribution")
	require.NotNil(t, distributionFlag, "expected --distribution flag (hidden)")
	require.True(t, distributionFlag.Hidden, "--distribution should be hidden")

	distributionConfigFlag := cmd.Flags().Lookup("distribution-config")
	require.NotNil(t, distributionConfigFlag, "expected --distribution-config flag (hidden)")
	require.True(t, distributionConfigFlag.Hidden, "--distribution-config should be hidden")

	gitopsEngineFlag := cmd.Flags().Lookup("gitops-engine")
	require.NotNil(t, gitopsEngineFlag, "expected --gitops-engine flag (hidden)")
	require.True(t, gitopsEngineFlag.Hidden, "--gitops-engine should be hidden")

	localRegistryFlag := cmd.Flags().Lookup("local-registry")
	require.NotNil(t, localRegistryFlag, "expected --local-registry flag (hidden)")
	require.True(t, localRegistryFlag.Hidden, "--local-registry should be hidden")
}
