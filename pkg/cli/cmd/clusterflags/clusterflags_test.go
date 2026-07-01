package clusterflags_test

import (
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/clusterflags"
	ksailconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterMirrorRegistryFlag(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	clusterflags.RegisterMirrorRegistryFlag(cmd)

	flag := cmd.Flags().Lookup("mirror-registry")
	require.NotNil(t, flag, "mirror-registry flag should be registered")
	assert.Equal(t, "stringSlice", flag.Value.Type())
}

func TestRegisterNameFlag(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cfgManager := ksailconfigmanager.NewCommandConfigManager(cmd, nil)
	clusterflags.RegisterNameFlag(cmd, cfgManager)

	flag := cmd.Flags().Lookup("name")
	require.NotNil(t, flag, "name flag should be registered")
	assert.Equal(t, "n", flag.Shorthand, "name flag exposes the -n shorthand")

	// The flag is bound to Viper, so a set value is readable through the manager.
	require.NoError(t, cmd.Flags().Set("name", "prod"))
	assert.Equal(t, "prod", cfgManager.Viper.GetString("name"))
}

func TestRegisterOIDCExtraScopeFlag(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	clusterflags.RegisterOIDCExtraScopeFlag(cmd)

	flag := cmd.Flags().Lookup("oidc-extra-scope")
	require.NotNil(t, flag, "oidc-extra-scope flag should be registered")
	assert.Equal(t, "stringSlice", flag.Value.Type())
}

func TestRegisterAllowedCIDRsFlag(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	clusterflags.RegisterAllowedCIDRsFlag(cmd)

	flag := cmd.Flags().Lookup("allowed-cidrs")
	require.NotNil(t, flag, "allowed-cidrs flag should be registered")
	assert.Equal(t, "stringSlice", flag.Value.Type())
}

func TestApplyClusterMutationFlags_MergesChangedFlags(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	clusterflags.RegisterOIDCExtraScopeFlag(cmd)
	clusterflags.RegisterAllowedCIDRsFlag(cmd)

	require.NoError(t, cmd.Flags().Set("oidc-extra-scope", "groups,email"))
	require.NoError(t, cmd.Flags().Set("allowed-cidrs", "203.0.113.0/24"))

	cfg := &v1alpha1.Cluster{}
	clusterflags.ApplyClusterMutationFlags(cmd, cfg)

	assert.Equal(t, []string{"groups", "email"}, cfg.Spec.Cluster.OIDC.ExtraScopes)
	assert.Equal(t, []string{"203.0.113.0/24"}, cfg.Spec.Provider.Hetzner.AllowedCIDRs)
}

func TestApplyClusterMutationFlags_LeavesUnsetFlagsUntouched(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	clusterflags.RegisterOIDCExtraScopeFlag(cmd)
	clusterflags.RegisterAllowedCIDRsFlag(cmd)

	cfg := &v1alpha1.Cluster{}
	cfg.Spec.Cluster.OIDC.ExtraScopes = []string{"preexisting"}
	clusterflags.ApplyClusterMutationFlags(cmd, cfg)

	// Flags were never set (.Changed is false), so the config is left as-is.
	assert.Equal(t, []string{"preexisting"}, cfg.Spec.Cluster.OIDC.ExtraScopes)
	assert.Empty(t, cfg.Spec.Provider.Hetzner.AllowedCIDRs)
}
