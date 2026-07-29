package cluster_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/project"
	projectenv "github.com/devantler-tech/ksail/v7/pkg/cli/cmd/project/env"
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

// TestClusterCmd_RegistersDeprecatedAddEnvironmentAlias guards the backward
// compatibility alias kept when `add-environment` moved to the `project` group
// (issue #5626). The previously released `ksail cluster add-environment` must
// keep working for one deprecation cycle: it stays wired to the shared
// project.NewAddEnvironmentCmd, marked Hidden (so it is absent from help, docs,
// and the MCP/chat tool surface), and carries the deprecation notice pointing at
// the new location. A silent regression of any of these would break existing
// users' invocations without a compile error.
func TestClusterCmd_RegistersDeprecatedAddEnvironmentAlias(t *testing.T) {
	t.Parallel()

	clusterCmd := cluster.NewClusterCmd()
	require.NotNil(t, clusterCmd)

	alias := findClusterSubcommand(clusterCmd, "add-environment")
	require.NotNil(t, alias, "expected 'add-environment' alias to stay registered under cluster")

	require.True(t, alias.Hidden, "cluster add-environment alias must be Hidden")
	require.Equal(t,
		`use "ksail project env add" instead`,
		alias.Deprecated,
		"alias must carry the deprecation notice pointing at the project env group",
	)

	// Delegation: the alias is the shared env command with the historical flat
	// name restored, so its Short must match the canonical projectenv.NewAddCmd.
	canonical := projectenv.NewAddCmd()

	require.Equal(
		t,
		"add-environment <name>",
		alias.Use,
		"alias must keep the previously released flat name",
	)
	require.Equal(
		t,
		canonical.Short,
		alias.Short,
		"alias must delegate to projectenv.NewAddCmd",
	)
}

// TestClusterCmd_RegistersDeprecatedInitAlias guards the backward compatibility
// alias kept when `init` moved to the `project` group (issue #5626). The
// previously released `ksail cluster init` must keep working for one deprecation
// cycle: it stays wired to the shared project.NewInitCmd, marked Hidden (so it is
// absent from help, docs, and the MCP/chat tool surface), and carries the
// deprecation notice pointing at the new location. A silent regression of any of
// these would break existing users' invocations without a compile error.
func TestClusterCmd_RegistersDeprecatedInitAlias(t *testing.T) {
	t.Parallel()

	clusterCmd := cluster.NewClusterCmd()
	require.NotNil(t, clusterCmd)

	alias := findClusterSubcommand(clusterCmd, "init")
	require.NotNil(t, alias, "expected 'init' alias to stay registered under cluster")

	require.True(t, alias.Hidden, "cluster init alias must be Hidden")
	require.Equal(t,
		`use "ksail project init" instead`,
		alias.Deprecated,
		"alias must carry the deprecation notice pointing at the project group",
	)

	// Delegation: the alias is the shared project command, so its Use and Short
	// must match the canonical project.NewInitCmd.
	canonical := project.NewInitCmd()
	require.Equal(t, canonical.Use, alias.Use, "alias must delegate to project.NewInitCmd")
	require.Equal(t, canonical.Short, alias.Short, "alias must delegate to project.NewInitCmd")
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
