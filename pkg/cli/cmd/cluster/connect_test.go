package cluster_test

import (
	"os"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/stretchr/testify/require"
)

// TestConnectAllowsLegacyTalosISO reaches k9s help without regenerating machine configuration.
//
//nolint:paralleltest // changes the process working directory
func TestConnectAllowsLegacyTalosISO(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile("ksail.yaml", []byte(`apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: legacy-image
spec:
  cluster:
    distribution: Talos
    provider: Hetzner
    talos:
      iso: 123456
`), 0o600))

	cmd := cluster.NewConnectCmd()
	cmd.SetContext(t.Context())
	require.NoError(t, cmd.RunE(cmd, []string{"--help"}))
}

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
