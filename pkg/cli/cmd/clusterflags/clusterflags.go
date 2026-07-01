// Package clusterflags provides the shared cluster-configuration flag helpers
// used by every command that takes cluster-spec input on the CLI — the cluster
// lifecycle commands (create, update, diff) and the project-scaffolding command
// (init). It registers the flags that populate a [v1alpha1.Cluster] and merges the
// non-Viper flag overrides back into a loaded config.
//
// The helpers live in their own neutral package (rather than in the `cluster`
// command package) so both the `cluster` and `project` command groups can use them
// without an import cycle: `cluster` imports `project` to expose the moved
// file-only commands as hidden deprecated aliases, so `project` cannot import
// `cluster` back — a shared, group-neutral home is the only cycle-free option.
package clusterflags

import (
	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	ksailconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/spf13/cobra"
)

// RegisterMirrorRegistryFlag adds the --mirror-registry flag to a command.
// The flag is intentionally NOT bound to Viper to allow custom merge logic
// via getMirrorRegistriesWithDefaults() in setup/mirrorregistry.
func RegisterMirrorRegistryFlag(cmd *cobra.Command) {
	cmd.Flags().StringSlice("mirror-registry", []string{},
		"Configure mirror registries with optional authentication. Format: [user:pass@]host[=upstream]. "+
			"Credentials support environment variables using ${VAR} syntax (quote placeholders so KSail can expand them). "+
			"Examples: docker.io=https://registry-1.docker.io, '${USER}:${TOKEN}@ghcr.io=https://ghcr.io'")
}

// RegisterNameFlag adds the --name flag to a command and binds it to Viper.
func RegisterNameFlag(cmd *cobra.Command, cfgManager *ksailconfigmanager.ConfigManager) {
	cmd.Flags().StringP("name", "n", "",
		"Cluster name used for container names, registry names, and kubeconfig context")
	_ = cfgManager.Viper.BindPFlag("name", cmd.Flags().Lookup("name"))
}

// RegisterOIDCExtraScopeFlag adds the --oidc-extra-scope flag to a command.
// Like --mirror-registry, this is a string slice flag that is NOT bound to Viper
// and instead merged manually after config loading.
func RegisterOIDCExtraScopeFlag(cmd *cobra.Command) {
	cmd.Flags().StringSlice("oidc-extra-scope", []string{},
		"Additional OIDC scopes beyond openid (repeatable)")
}

// applyOIDCExtraScopeFlag merges --oidc-extra-scope flag values into the cluster config.
// CLI flag values take precedence over config file values when explicitly set.
func applyOIDCExtraScopeFlag(cmd *cobra.Command, clusterCfg *v1alpha1.Cluster) {
	scopeFlag := cmd.Flags().Lookup("oidc-extra-scope")
	if scopeFlag == nil || !scopeFlag.Changed {
		return
	}

	scopes, err := cmd.Flags().GetStringSlice("oidc-extra-scope")
	if err != nil {
		return
	}

	// When the flag is explicitly set, always assign — even if empty — so the
	// user can clear extraScopes from a config file via CLI.
	clusterCfg.Spec.Cluster.OIDC.ExtraScopes = scopes
}

// RegisterAllowedCIDRsFlag adds the --allowed-cidrs flag to a command.
// Like --mirror-registry, this is a string slice flag NOT bound to Viper
// and merged manually via applyAllowedCIDRsFlag.
func RegisterAllowedCIDRsFlag(cmd *cobra.Command) {
	cmd.Flags().StringSlice("allowed-cidrs", []string{},
		"CIDR blocks allowed to access the Kubernetes API and Talos API on control-plane nodes. "+
			"When empty, both APIs are open to 0.0.0.0/0 and ::/0 (all IPv4 and IPv6). "+
			"Example: --allowed-cidrs 203.0.113.0/24 --allowed-cidrs 198.51.100.0/24")
}

// applyAllowedCIDRsFlag merges --allowed-cidrs flag values into the cluster config.
// CLI flag values take precedence over config file values when explicitly set.
func applyAllowedCIDRsFlag(cmd *cobra.Command, clusterCfg *v1alpha1.Cluster) {
	cidrFlag := cmd.Flags().Lookup("allowed-cidrs")
	if cidrFlag == nil || !cidrFlag.Changed {
		return
	}

	cidrs, err := cmd.Flags().GetStringSlice("allowed-cidrs")
	if err != nil {
		return
	}

	clusterCfg.Spec.Provider.Hetzner.AllowedCIDRs = cidrs
}

// ApplyClusterMutationFlags merges the non-Viper CLI flag overrides
// (--oidc-extra-scope and --allowed-cidrs) into the cluster config. Centralizing
// the set keeps every mutation command (create, update, init) applying the same
// flags; a new manually-merged flag is added here once rather than at each call site.
func ApplyClusterMutationFlags(cmd *cobra.Command, clusterCfg *v1alpha1.Cluster) {
	applyOIDCExtraScopeFlag(cmd, clusterCfg)
	applyAllowedCIDRsFlag(cmd, clusterCfg)
}
