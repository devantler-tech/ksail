package cluster_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// TestClusterCmd_RegistersOIDCSubcommand guards the relocation of the OIDC
// command from `ksail oidc` to `ksail cluster oidc`. It must stay wired as a
// cluster subcommand with its `get-token` child, because generated kubeconfig
// exec credentials invoke `ksail cluster oidc get-token`; silent
// de-registration would break OIDC authentication without any compile error.
func TestClusterCmd_RegistersOIDCSubcommand(t *testing.T) {
	t.Parallel()

	clusterCmd := cluster.NewClusterCmd()
	require.NotNil(t, clusterCmd)

	oidcCmd := findClusterSubcommand(clusterCmd, "oidc")
	require.NotNil(t, oidcCmd, "expected 'oidc' subcommand to be registered under cluster")

	getTokenCmd := findClusterSubcommand(oidcCmd, "get-token")
	require.NotNil(t, getTokenCmd, "expected 'get-token' subcommand under 'cluster oidc'")
}

// findClusterSubcommand returns the named direct subcommand of parent, or nil.
func findClusterSubcommand(parent *cobra.Command, name string) *cobra.Command {
	for _, sub := range parent.Commands() {
		if sub.Name() == name {
			return sub
		}
	}

	return nil
}
